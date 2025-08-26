package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"iter"
	"log"
	"os"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/llm/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

var (
	logfile   string
	resume    bool
	groot     string
	sessionID string
)

func main() {
	ctx := context.Background()
	flag.StringVar(&logfile, "logfile", "adk_runner.log", "")
	flag.BoolVar(&resume, "resume", false, "")
	flag.StringVar(&groot, "groot", "wss://dev-grootafe-pa-googleapis.sandbox.google.com/ws/cloud.ai.groot.afe.GRootAfeService/ExecuteActions", "")
	flag.StringVar(&sessionID, "sid", "", "")
	flag.Parse()

	model, err := gemini.NewModel(ctx, "gemini-2.5-flash", &genai.ClientConfig{
		APIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	agent, err := llmagent.New(llmagent.Config{
		Name:        "weather_time_agent",
		Model:       model,
		Description: "Agent to answer questions about the time and weather in a city.",
		Instruction: "I can answer your questions about the time and weather in a city.",
		Tools: []tool.Tool{
			tool.NewGoogleSearchTool(model),
		},
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	var r Runner
	rconf := &runner.GRootRunnerConfig{
		GRootEndpoint:  groot,
		GRootAPIKey:    os.Getenv("GROOT_KEY"),
		GRootSessionID: sessionID,
		EventLog:       logfile,
		AppName:        "hello_world",
		RootAgent:      agent,
	}
	if !resume {
		r, err = runner.NewGRootRunner(rconf)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		r, err = runner.NewResumerGRootRunner(rconf)
		if err != nil {
			log.Fatal(err)
		}
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nUser -> ")

		userInput, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		userMsg := genai.NewContentFromText(userInput, genai.RoleUser)
		fmt.Print("\n")
		for event, err := range r.Run(ctx, "test_user", sessionID, userMsg, &runner.RunConfig{
			StreamingMode: runner.StreamingModeSSE,
		}) {
			if err != nil {
				fmt.Printf("\nAGENT_ERROR: %v\n", err)
			} else {
				for _, p := range event.LLMResponse.Content.Parts {
					fmt.Print(p.Text)
				}
			}
		}
	}
}

type Runner interface {
	Run(ctx context.Context, userID, sessionID string, msg *genai.Content, cfg *runner.RunConfig) iter.Seq2[*session.Event, error]
}
