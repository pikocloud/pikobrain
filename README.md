# PikoBrain

PikoBrain is function-calling API for LLM from multiple providers.

The key project features:

- allows you to define model configuration
- provides universal API regardless of LLM
- provides actual function calling (currently OpenAPI)

It allows set functions (RAG) without vendor lock-in.

The project LICENSED under MPL-2.0 Exhibit A which promotes collaboration (requires sharing changes) but does not
restrict for commercial or any other usage.

## Roadmap

Providers

- [ ] AWS Bedrock
- [ ] Ollama

State

- [ ] Threads

Integration

- [ ] Webhooks
- [ ] NATS Notifications

Functions

- [ ] Internal functions (threads)
- [ ] Scripting functions

## Installation

- Source (requires go 1.22.5+) `go run github.com/pikocloud/pikobrain@latest <args>`
- Binary in [releases](releases)
- Docker `ghcr.io/pikocloud/pikobrain`

## Usage

Binary

    pikobrain --config examples/brain.yaml --tools examples/tools.yaml

Docker

    docker run --rm -v $(pwd)/examples:/config:ro -p 8080:8080 ghcr.io/pikocloud/pikobrain

- Define model and tools like in [examples/](examples/)
- Run service
- Call service

Input can be:

- `multipart/form-data payload` (preferred), where:
    - each part can be text/plain (default if not set), application/x-www-form-urlencoded, application/json, image/png,
      image/jpeg, image/webp, image/gif
    - may contain header `X-User` in each part which maps to user field in providers
    - may contain header `X-Role` where values could be `user` (default) or `assistant`
    - multipart name doesn't matter
- `application/x-www-form-urlencoded`; content will be decoded
- `text/plain`, `application/json`
- `image/png`, `image/jpeg`, `image/webp`, `image/gif`
- without content type, then payload should be valid UTF-8 string and will be used as single payload

> Request may contain query parameter `user` which maps to user field and/or query `role` (user or assistant)

Multipart payload allows caller provide full history context messages. For multipart, header `X-User` and `X-Role` may
override query parameters.

Output is the response from LLM.

### Examples

Simple

    curl --data 'Why sky is blue?' http://127.0.0.1:8080

Text multipart

    curl -F '_=my name is RedDec' -F '_=What is my name?' -v http://127.0.0.1:8080

Image and text

    curl -F '_=@eifeltower.jpeg' -F '_=Describe the picture' -v http://127.0.0.1:8080

## CLI

```
Application Options:
      --timeout=                  LLM timeout (default: 30s) [$TIMEOUT]
      --refresh=                  Refresh interval for tools (default: 30s) [$REFRESH]
      --config=                   Config file (default: brain.yaml) [$CONFIG]
      --tools=                    Tool file (default: tools.yaml) [$TOOLS]

Debug:
      --debug.enable              Enable debug mode [$DEBUG_ENABLE]

HTTP server configuration:
      --http.bind=                Bind address (default: :8080) [$HTTP_BIND]
      --http.tls                  Enable TLS [$HTTP_TLS]
      --http.ca=                  Path to CA files. Optional unless IGNORE_SYSTEM_CA set (default: ca.pem) [$HTTP_CA]
      --http.cert=                Server certificate (default: cert.pem) [$HTTP_CERT]
      --http.key=                 Server private key (default: key.pem) [$HTTP_KEY]
      --http.mutual               Enable mutual TLS [$HTTP_MUTUAL]
      --http.ignore-system-ca     Do not load system-wide CA [$HTTP_IGNORE_SYSTEM_CA]
      --http.read-header-timeout= How long to read header from the request (default: 3s) [$HTTP_READ_HEADER_TIMEOUT]
      --http.graceful=            Graceful shutdown timeout (default: 5s) [$HTTP_GRACEFUL]
      --http.timeout=             Any request timeout (default: 30s) [$HTTP_TIMEOUT]
      --http.max-body-size=       Maximum payload size in bytes (default: 1048576) [$HTTP_MAX_BODY_SIZE]
```