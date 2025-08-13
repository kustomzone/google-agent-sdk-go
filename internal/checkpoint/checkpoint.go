package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log"
	"sync"
	"time"

	"google.golang.org/adk/session"
	"google.golang.org/adk/types"
)

type Node struct {
	ID   string
	Deps []string
	Run  func(*CheckpointContext) iter.Seq2[*session.Event, error]
}

type NodeResult struct {
	NodeID      string    `json:"node_id"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Events      []*session.Event
}

type CheckpointContext struct {
	InvocationContext types.InvocationContext
	Spawn             func(Node) error
}

type Checkpointer interface {
	Save(ctx context.Context, nodeID string, res NodeResult) error
}

type InMemoryCheckpointer struct {
	mu    sync.Mutex
	store map[string]NodeResult
}

func MemCheckpointer() *InMemoryCheckpointer {
	return &InMemoryCheckpointer{store: make(map[string]NodeResult)}
}

func (cp *InMemoryCheckpointer) Save(ctx context.Context, nodeID string, res NodeResult) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	cp.store[nodeID] = res
	return nil
}

func (cp *InMemoryCheckpointer) Get(nodeID string) (NodeResult, bool) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	r, ok := cp.store[nodeID]
	return r, ok
}

// Executor supports dynamic DAGs.
type Executor struct {
	MaxParallelism  int
	Checkpointer    Checkpointer
	StopOnFirstFail bool

	mu       sync.Mutex
	ictx     *types.InvocationContext
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	cond     *sync.Cond
	wg       sync.WaitGroup
	shutdown bool
	failErr  error

	nodes    map[string]*Node
	waiting  map[string]map[string]struct{}
	children map[string]map[string]struct{}
	readyQ   []string
	running  map[string]struct{}
	done     map[string]struct{}
}

func NewExecutor() *Executor {
	ex := &Executor{
		Checkpointer: MemCheckpointer(),

		nodes:    make(map[string]*Node),
		waiting:  make(map[string]map[string]struct{}),
		children: make(map[string]map[string]struct{}),
		running:  make(map[string]struct{}),
		done:     make(map[string]struct{}),
	}
	ex.cond = sync.NewCond(&ex.mu)
	return ex
}

func (ex *Executor) Start(ictx *types.InvocationContext) {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if ex.started {
		return
	}
	if ex.MaxParallelism <= 0 {
		ex.MaxParallelism = 1
	}
	ex.ictx = ictx
	ex.ctx, ex.cancel = context.WithCancel(context.Background())
	ex.started = true
	for i := 0; i < ex.MaxParallelism; i++ {
		ex.wg.Add(1)
		go ex.worker()
	}
}

func (ex *Executor) Stop() {
	ex.mu.Lock()
	if ex.shutdown {
		ex.mu.Unlock()
		return
	}
	ex.shutdown = true
	if ex.cancel != nil {
		ex.cancel()
	}
	ex.cond.Broadcast()
	ex.mu.Unlock()
	ex.wg.Wait()
}

func (ex *Executor) Spawn(n Node) error {
	if n.ID == "" {
		return errors.New("node ID cannot be empty")
	}
	ex.mu.Lock()
	defer func() {
		ex.cond.Broadcast()
		ex.mu.Unlock()
	}()
	if _, dup := ex.nodes[n.ID]; dup || ex.isFinishedLocked(n.ID) {
		return fmt.Errorf("node %q already exists or finished", n.ID)
	}
	nc := n
	ex.nodes[n.ID] = &nc

	need := make(map[string]struct{})
	for _, d := range n.Deps {
		if !ex.isFinishedLocked(d) {
			need[d] = struct{}{}
			if ex.children[d] == nil {
				ex.children[d] = make(map[string]struct{})
			}
			ex.children[d][n.ID] = struct{}{}
		}
	}
	if len(need) == 0 {
		ex.readyQ = append(ex.readyQ, n.ID)
	} else {
		ex.waiting[n.ID] = need
	}
	return nil
}

func (ex *Executor) worker() {
	defer ex.wg.Done()
	for {
		ex.mu.Lock()
		for !ex.shutdown && len(ex.readyQ) == 0 {
			if ex.StopOnFirstFail && ex.failErr != nil {
				ex.mu.Unlock()
				return
			}
			ex.cond.Wait()
			if ex.ctx.Err() != nil {
				ex.mu.Unlock()
				return
			}
		}
		if ex.shutdown || (ex.StopOnFirstFail && ex.failErr != nil) {
			ex.mu.Unlock()
			return
		}
		id := ex.readyQ[len(ex.readyQ)-1]
		ex.readyQ = ex.readyQ[:len(ex.readyQ)-1]
		ex.running[id] = struct{}{}
		n := ex.nodes[id]
		ex.mu.Unlock()

		ex.runOne(id, n)
	}
}

func (ex *Executor) runOne(id string, n *Node) {
	start := time.Now()
	var eventsSeen int
	var runErr error
	if n.Run == nil {
		runErr = errors.New("nil Run")
	} else {
		spawn := func(newNode Node) error {
			return ex.Spawn(newNode)
		}
		cctx := &CheckpointContext{
			InvocationContext: *ex.ictx,
			Spawn:             spawn,
		}
		n.Run(cctx)(func(ev *session.Event, err error) bool {
			if err != nil {
				runErr = err
				return false
			}
			if ev != nil {
				eventsSeen++
			}
			select {
			case <-ex.ctx.Done():
				runErr = ex.ctx.Err()
				return false
			default:
				return true
			}
		})
	}

	res := NodeResult{
		NodeID:      id,
		StartedAt:   start,
		CompletedAt: time.Now(),
	}
	if ex.Checkpointer != nil && runErr == nil {
		if err := ex.Checkpointer.Save(ex.ctx, id, res); err != nil {
			log.Printf("checkpoint save failed for %s: %v", id, err)
		}
	}

	ex.mu.Lock()
	delete(ex.running, id)
	ex.done[id] = struct{}{}

	if kids := ex.children[id]; kids != nil {
		for child := range kids {
			if need := ex.waiting[child]; need != nil {
				delete(need, id)
				if len(need) == 0 {
					delete(ex.waiting, child)
					ex.readyQ = append(ex.readyQ, child)
				}
			}
		}
	}
	if runErr != nil && ex.StopOnFirstFail && ex.failErr == nil {
		ex.failErr = fmt.Errorf("node %s failed: %w", id, runErr)
		if ex.cancel != nil {
			ex.cancel()
		}
	}
	ex.cond.Broadcast()
	ex.mu.Unlock()
}

func (ex *Executor) isFinishedLocked(id string) bool {
	_, ok := ex.done[id]
	return ok
}

func (ex *Executor) Wait(ctx context.Context) error {
	t := time.NewTicker(25 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			ex.mu.Lock()
			idle := len(ex.readyQ) == 0 && len(ex.running) == 0 && len(ex.waiting) == 0
			err := ex.failErr
			ex.mu.Unlock()
			if err != nil && ex.StopOnFirstFail {
				return err
			}
			if idle {
				return nil
			}
		}
	}
}
