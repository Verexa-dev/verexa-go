# verexa (Go)

Checks every prompt and model response against your Verexa policy before it reaches the model or the user. Zero dependencies outside the standard library. Go 1.22+.

```bash
go get github.com/Verexa-dev/verexa-go
```

Create a key in the dashboard under **Settings → API keys** and set it in the environment:

```bash
VEREXA_API_KEY=vx_live_...
```

`VEREXA_BASE_URL` is optional and defaults to `https://api.verexa.dev`. Set it to use a local or self-hosted verdict-api.

## OpenAI

`Transport` wraps any `http.RoundTripper` and guards OpenAI-compatible traffic, so it works with any client that takes an `*http.Client`.

```go
import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	verexa "github.com/Verexa-dev/verexa-go"
)

guard := verexa.New(verexa.Config{})
client := openai.NewClient(
	option.WithHTTPClient(&http.Client{Transport: guard.Transport(nil)}),
)
```

With `sashabaranov/go-openai`, set `config.HTTPClient` the same way.

Every `POST .../chat/completions` and `POST .../responses` is now checked on both sides of the turn. A blocked turn returns a `*verexa.BlockedError`; recover it with `errors.As`:

```go
_, err := client.Chat.Completions.New(ctx, params)
var blocked *verexa.BlockedError
if errors.As(err, &blocked) {
	// blocked.Phase is "input" or "output"; blocked.Response is the full verdict.
}
```

A blocked input never reaches the provider. A redacted input reaches it with the sensitive span removed, and a redacted reply comes back with the span removed.

## Manual

```go
guard := verexa.New(verexa.Config{})

input := guard.CheckInput(ctx, userMessage, verexa.CheckOptions{TraceID: traceID, Profile: "balanced"})
if input.Action == verexa.ActionBlock {
	return
}
prompt := userMessage
if input.Action == verexa.ActionRedact {
	prompt = input.Text
}

reply := callModel(prompt)

output := guard.CheckOutput(ctx, reply, verexa.CheckOptions{TraceID: traceID, SystemPrompt: systemPrompt})
if output.Action == verexa.ActionBlock {
	return
}
if output.Action == verexa.ActionRedact {
	reply = output.Text
}
send(reply)
```

## Behaviour worth knowing

- **Fails open by default.** `Check` never returns an error. If the service is unreachable the verdict is `allow` with `Degraded: true`, and a circuit breaker stops hammering it. Set `FailMode: verexa.FailClosed` to block instead.
- **One trace id per turn.** The transport's input and output checks share one, so the dashboard shows them as one trace.
- **Streaming is checked after the fact.** Chunks reach the caller as they arrive. When the stream ends, the assembled reply is checked, and a blocking verdict replaces the final event with a `*BlockedError` from the body's `Read` (`stream.Err()` in openai-go). It is a retraction signal, not a gate.
- **Retries.** openai-go retries failed round trips, including a block. The transport remembers blocked turns, so a retry gets the same `*BlockedError` back without calling the model or the guard again. openai-go still waits out its backoff first (about 1.2s with the default two retries) before returning the error; `option.WithMaxRetries` controls that.
- **Profiles.** `deterministic` (pattern detectors only, no model calls), `balanced` (adds the tier-2 classifier, and the tier-3 judge on anything they do not call clean), `audit` (the judge reviews every check; set `CheckOptions.Timeout` to around 15s). A check that names no profile uses the project's default from the dashboard, and that is what `Transport` sends.
- **`option.WithUnsafeAllowHTTP` bypasses the guard.** For loopback URLs openai-go then sends requests through its own transport, not yours. Use HTTPS, even locally.

## Tests

```bash
go test -race ./...                  # the SDK, standard library only
(cd compat && go test -race ./...)   # the transport driven by the real openai-go client
```

`examples/external-integration-go` runs live attack scenarios against a running verdict-api.
