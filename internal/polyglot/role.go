package polyglot

import "strings"

// serverSignals are code patterns that indicate a server/API application.
var serverSignals = []string{
	// Go
	"http.ListenAndServe", "http.Serve", "gin.Default", "echo.New", "chi.NewRouter",
	// Node.js / TypeScript
	"express()", "fastify()", "Hono(", "createServer(",
	// Python
	"Flask(__name__)", "FastAPI()", "Django", "uvicorn.run",
	// Java
	"SpringApplication.run", "SpringBootApplication", "HttpServer::new",
	// Rust
	"axum::Router", "rocket::build",
	// Ruby
	"Sinatra::Base", "Rails.application",
	// C#
	"WebApplication.CreateBuilder", "Host.CreateDefaultBuilder",
}

// clientSignals are code patterns that indicate a frontend/client application.
var clientSignals = []string{
	"ReactDOM", "createRoot", "document.querySelector", "document.getElementById",
	"fetch(", "axios.", "XMLHttpRequest", "Vue.createApp", "createApp(", "angular.module",
}

// workerSignals are code patterns that indicate an ML/data-processing worker.
var workerSignals = []string{
	"import torch", "import tensorflow", "from sklearn", "import pandas",
	"torch.nn", "tf.keras", "model.fit", "model.train",
}

// classifyRole inspects source code snippets for known framework/library
// signals and returns the most likely role: "server", "client", "worker",
// or "library" (default when no signal matches).
func classifyRole(sources []string) string {
	joined := strings.Join(sources, "\n")

	for _, sig := range serverSignals {
		if strings.Contains(joined, sig) {
			return "server"
		}
	}

	for _, sig := range clientSignals {
		if strings.Contains(joined, sig) {
			return "client"
		}
	}

	for _, sig := range workerSignals {
		if strings.Contains(joined, sig) {
			return "worker"
		}
	}

	return "library"
}

// ClassifyLayerRole is the public entry point for role classification.
// It takes source code snippets from a single layer and returns one of:
// "server", "client", "worker", or "library".
func ClassifyLayerRole(sources []string) string {
	return classifyRole(sources)
}
