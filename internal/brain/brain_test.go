package brain_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pikocloud/pikobrain/internal/brain"
	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/utils"
	"github.com/pikocloud/pikobrain/testutils"
)

func TestMain(m *testing.M) {
	testutils.LoadEnv()
	testutils.SetupLogging()
	os.Exit(m.Run())
}

func userMessage(name string, content string) types.Message {
	return types.Message{
		Role: types.RoleUser,
		User: name,
		Content: types.Content{
			Mime: types.MIMEText,
			Data: []byte(content),
		},
	}
}

func TestOllama(t *testing.T) {
	testBrain(t, brain.Definition{
		Config: types.Config{
			Model:     "mistral:instruct",
			Prompt:    "Your are the helpful assistant",
			MaxTokens: 300,
		},
		Vision: &brain.Vision{
			Model: "llava",
		},
		MaxIterations: 2,
		Provider:      "ollama",
		URL:           "http://localhost:11434",
	})
}

func TestAWS(t *testing.T) {
	testBrain(t, brain.Definition{
		Config: types.Config{
			Model:     "anthropic.claude-3-5-sonnet-20240620-v1:0",
			Prompt:    "Your are the helpful assistant",
			MaxTokens: 300,
		},
		MaxIterations: 2,
		Provider:      "bedrock",
	})
}

func TestOpenAI(t *testing.T) {
	testBrain(t, brain.Definition{
		Config: types.Config{
			Model:     "gpt-4o-mini",
			Prompt:    "Your are the helpful assistant",
			MaxTokens: 300,
		},
		MaxIterations: 2,
		URL:           "https://api.openai.com/v1",
		Provider:      "openai",
		Secret:        utils.Value[string]{FromEnv: "OPENAI_TOKEN"},
	})
}

func testBrain(t *testing.T, definition brain.Definition) {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Minute)
	defer cancel()

	type WeatherRequest struct {
		Planet string `json:"planet" jsonschema:"description=Planet name"`
	}

	var tools types.DynamicToolbox
	tools.Add(types.MustTool("get_weather_on_planet", "Get weather on any planet in realtime", func(ctx context.Context, payload WeatherRequest) (types.Content, error) {
		return types.Text("135"), nil
	}))
	err := tools.Update(ctx, true)
	require.NoError(t, err)

	b, err := brain.New(ctx, &tools, definition)
	require.NoError(t, err)

	t.Run("simple", func(t *testing.T) {
		out, err := b.Run(ctx, []types.Message{
			userMessage("reddec", "Why sky is blue?"),
		})
		require.NoError(t, err)

		var found bool
	search:
		for _, msg := range out {
			for _, content := range msg.Output {
				if strings.Contains(content.Content.String(), "scatter") {
					found = true
					break search
				}
			}
		}
		require.True(t, found, "response should contain 'scatter'")
	})

	t.Run("tool_call", func(t *testing.T) {
		out, err := b.Run(ctx, []types.Message{
			userMessage("reddec", "What is the temperature on planet Venus today?"),
		})
		require.NoError(t, err)
		dumpOutput(t, out)

		require.Equal(t, 1, out.Called("get_weather_on_planet"))
		reply := out.Reply()

		var found bool
	search:
		for _, r := range out {
			for _, o := range r.Output {
				if o.Role == types.RoleAssistant {
					found = strings.Contains(o.Content.String(), "135")
					if found {
						break search
					}
				}
			}
		}
		require.True(t, found, "response should contain '135'")
		t.Logf("%s", reply)
	})

	t.Run("multi_model", func(t *testing.T) {
		imagRes, err := http.Get("https://free-images.com/sm/da59/paris_france_eiffel_eiffel.jpg")
		require.NoError(t, err)
		defer imagRes.Body.Close()
		require.Equal(t, http.StatusOK, imagRes.StatusCode)
		picture, err := io.ReadAll(imagRes.Body)
		require.NoError(t, err)

		out, err := b.Run(ctx, []types.Message{
			{
				Role: types.RoleUser,
				User: "reddec",
				Content: types.Content{
					Data: picture,
					Mime: types.MIMEJpg,
				},
			},
			userMessage("reddec", "Describe image"),
		})
		require.NoError(t, err)

		dumpOutput(t, out)

		reply := out.Reply()

		t.Logf("%s", reply)
		require.Contains(t, strings.ToLower(string(reply.Data)), "eiffel")
	})
}

func dumpOutput(t *testing.T, out brain.Response) {
	for _, msg := range out {
		for _, content := range msg.Output {
			t.Logf("%s: %s: %s", content.Role, content.Content.Mime, content.Content.String())
		}
	}
}
