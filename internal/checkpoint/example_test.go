package checkpoint_test

import (
	"log"

	"google.golang.org/adk/internal/checkpoint"
	"google.golang.org/adk/session"
)

func Example() {
	manager := &checkpoint.Manager{
		InvocationID: "invocation-123",
		Branch:       "branch",
	}
	// Run runs the "preprocess" operation if it's not already completed.
	// If completed, it hydrates the operation from the checkpointed events.
	// After manager.Run is completed, a checkpoint is automatically created.
	// Returned events are used to read the newly created or hydrated events.
	it, status := manager.Run("preprocess", func(append checkpoint.AppendFunc) error {
		// Append one of more events to the operation.
		append(&session.Event{}) // TODO: Append events as they are produced.
		return nil
	})
	for event, err := range it {
		if err != nil {
			log.Fatal(err)
		}
		_ = event // Use the newly produced or hydrated event
	}

	// TODO: Optionally check whether checkpoint succesfully stored.
	// Only is available after all events are emitted from Run.
	_ = status.Error()
}
