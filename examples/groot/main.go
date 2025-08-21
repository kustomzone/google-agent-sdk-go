package main

import (
	"context"
	"log"
	"os"

	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/llm/gemini"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

const endpoint = "wss://dev-grootafe-pa-googleapis.sandbox.google.com/ws/cloud.ai.groot.afe.GRootAfeService/ExecuteAction"

func main() {
	ctx := context.Background()

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

	r, err := runner.NewGRootRunner(endpoint, "hello", agent)
	if err != nil {
		log.Fatal(err)
	}
	r.Run(ctx, "user_1", "session_1", genai.Text("hello, agent!")[0], &runner.RunConfig{})
}
