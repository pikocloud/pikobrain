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

- [x] OpenAI
- [x] [AWS Bedrock](#aws-bedrock)
- [ ] Ollama

State

- [ ] Threads

Integration

- [ ] Webhooks
- [ ] NATS Notifications

Functions

- [x] OpenAPI (including automatic reload)
- [ ] Internal functions (threads)
- [ ] Scripting functions

Libraries

- [ ] Python
- [ ] Golang
- [ ] Typescript

## Installation

- Source (requires go 1.22.5+) `go run github.com/pikocloud/pikobrain@latest <args>`
- Binary in [releases](https://github.com/pikocloud/pikobrain/releases/latest)
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

### Clients

<details>
<summary>Python with aiohttp</summary>

```python3
import asyncio
import io
from dataclasses import dataclass
from datetime import timedelta
from typing import Literal, Iterable

import aiohttp


@dataclass(frozen=True, slots=True)
class Message:
    content: str | bytes | io.BytesIO
    mime: str | None = None
    role: Literal['assistant', "user"] | None = None
    user: str | None = None


@dataclass(frozen=True, slots=True)
class Response:
    content: bytes
    mime: str
    duration: timedelta
    input_messages: int
    input_tokens: int
    output_tokens: int
    total_tokens: int


async def request(url: str, messages: Iterable[Message]) -> Response:
    with aiohttp.MultipartWriter('form-data') as mpwriter:
        for message in messages:
            headers = {}
            if message.mime:
                headers[aiohttp.hdrs.CONTENT_TYPE] = message.mime
            if message.role:
                headers['X-Role'] = message.role
            if message.user:
                headers['X-User'] = message.user

            mpwriter.append(message.content, headers)

        async with aiohttp.ClientSession() as session, session.post(url, data=mpwriter) as res:
            assert res.ok, await res.text()
            return Response(
                content=await res.read(),
                mime=res.headers.get(aiohttp.hdrs.CONTENT_TYPE),
                duration=timedelta(seconds=float(res.headers.get('X-Run-Duration'))),
                input_messages=int(res.headers.get('X-Run-Context')),
                input_tokens=int(res.headers.get('X-Run-Input-Tokens')),
                output_tokens=int(res.headers.get('X-Run-Output-Tokens')),
                total_tokens=int(res.headers.get('X-Run-Total-Tokens')),
            )


async def example():
    res = await request('http://127.0.0.1:8080', messages=[
        Message("My name is RedDec. You name is Bot."),
        Message("What is your and my name?"),
    ])
    print(res)
```

</details>

#### cURL

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

## Providers

### OpenAI

First-class support, everything works just fine.

### AWS Bedrock

> [!WARNING]  
> Due to multiple limitations, only Claude 3+ models are working properly. Recommended multi-modal model for AWS
> Bedrock is Anthropic Claude-3-5.

Initial support.

- Some models may not support system prompt.
- Some models may not support tools.
- Authorization is ignored (use AWS environment variables)
- `forceJSON` is not supported (workaround: use tools)

Required minimal set of environment variables

    AWS_ACCESS_KEY_ID=
    AWS_SECRET_ACCESS_KEY=
    AWS_REGION=

Please refer
to [AWS Environment variable cheatsheet](https://docs.aws.amazon.com/sdkref/latest/guide/settings-reference.html#EVarSettings)
for configuration.

Based on [function calling feature](https://docs.aws.amazon.com/bedrock/latest/userguide/tool-use.html) the recommended
models are:

- Anthropic Claude 3 models
- Mistral AI Mistral Large and Mistral Small
- Cohere Command R and Command R+

See list of [compatibilities](https://docs.aws.amazon.com/bedrock/latest/userguide/conversation-inference.html)