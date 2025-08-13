package checkpoint_test

import (
	"context"
	"iter"
	"log"

	"google.golang.org/adk/internal/checkpoint"
	"google.golang.org/adk/session"
	"google.golang.org/adk/types"
)

func Example() {
	ctx := context.Background()

	ex := checkpoint.NewExecutor()
	ex.Start(&types.InvocationContext{
		InvocationID: "invocation-123",
	})
	defer ex.Stop()

	if err := ex.Spawn(checkpoint.Node{
		ID: "root",
		Run: func(cctx *checkpoint.CheckpointContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				// TODO: Yield events and spawn children.
				cctx.Spawn(checkpoint.Node{
					ID:   "agent123",
					Deps: []string{"root"}, // TODO: can automatically assign.
					Run: func(cctx *checkpoint.CheckpointContext) iter.Seq2[*session.Event, error] {
						// TODO: Yield events.
						panic("not implemented")
					},
				})
				cctx.Spawn(checkpoint.Node{
					ID:   "model123",
					Deps: []string{"root"},
					Run: func(cctx *checkpoint.CheckpointContext) iter.Seq2[*session.Event, error] {
						// TODO: Yield events.
						panic("not implemented")
					},
				})
			}
		},
	}); err != nil {
		log.Fatalf("Failed to create executor: %v", err)
	}
	if err := ex.Wait(ctx); err != nil { // blocks until the flow is completed.
		log.Fatalf("Failed to wait: %v", err)
	}
}
