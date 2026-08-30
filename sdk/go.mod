// The SDK is its own module on purpose. Living inside the server's module
// would make every consumer inherit the engine's dependency graph — GORM,
// goja, RabbitMQ, OpenTelemetry — to call an HTTP API. A client library that
// costs that much to import does not get imported.
module github.com/gsoultan/metis/sdk

go 1.27.0
