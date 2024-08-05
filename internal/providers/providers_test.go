package providers_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pikocloud/pikobrain/internal/providers/bedrock"
	"github.com/pikocloud/pikobrain/internal/providers/openai"
	"github.com/pikocloud/pikobrain/internal/providers/types"
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

func TestAWS(t *testing.T) {
	provider, err := bedrock.New(context.TODO())
	require.NoError(t, err)

	testProvider(t, provider, types.Config{
		Model:         "anthropic.claude-3-5-sonnet-20240620-v1:0",
		Prompt:        "Your are the helpful assistant",
		MaxTokens:     300,
		MaxIterations: 2,
	})
}

func TestOpenAI(t *testing.T) {
	chatGPT := openai.New("https://api.openai.com/v1", os.Getenv("OPENAI_TOKEN"))

	testProvider(t, chatGPT, types.Config{
		Model:         "gpt-4o-mini",
		Prompt:        "Your are the helpful assistant",
		MaxTokens:     300,
		MaxIterations: 2,
	})
}

func testProvider(t *testing.T, provider types.Provider, config types.Config) {
	ctx, cancel := context.WithTimeout(context.TODO(), time.Minute)
	defer cancel()

	type WeatherRequest struct {
		Planet string `json:"planet" jsonschema:"description=Planet name"`
	}

	var tools types.DynamicToolbox
	tools.Add(types.MustTool("get_weather_on_planet", "Get weather on any planet in realtime.", func(ctx context.Context, payload WeatherRequest) (types.Content, error) {
		assert.Equal(t, "Venus", payload.Planet)
		return types.Text("135"), nil
	}))
	err := tools.Update(ctx, true)
	require.NoError(t, err)

	t.Run("simple", func(t *testing.T) {
		out, err := provider.Execute(ctx, &types.Request{
			Config: config,
			History: []types.Message{
				userMessage("reddec", "Why sky is blue?"),
			},
			Tools: &tools,
		})
		require.NoError(t, err)
		require.Len(t, out.Messages, 1)

		for _, msg := range out.Messages {
			t.Logf("input: %s", string(msg.Input))
			t.Logf("output: %s", string(msg.Output))
			require.Len(t, msg.Content, 1)
			for _, content := range msg.Content {
				t.Logf("%s: %s", content.Mime, string(content.Data))
			}
		}
	})

	t.Run("tool_call", func(t *testing.T) {
		out, err := provider.Execute(ctx, &types.Request{
			Config: config,
			History: []types.Message{
				userMessage("reddec", "What's temperature in Venus today?"),
			},
			Tools: &tools,
		})
		require.NoError(t, err)
		require.Len(t, out.Messages, 2)

		for _, msg := range out.Messages {
			t.Logf("input: %s", string(msg.Input))
			t.Logf("output: %s", string(msg.Output))
		}
		require.Equal(t, 1, out.Called("get_weather_on_planet"))
		reply := out.Reply()

		require.Contains(t, string(reply.Data), "135")
		t.Logf("%s", reply)
	})

	t.Run("multi_model", func(t *testing.T) {
		imagRes, err := http.Get("https://free-images.com/sm/da59/paris_france_eiffel_eiffel.jpg")
		require.NoError(t, err)
		defer imagRes.Body.Close()
		require.Equal(t, http.StatusOK, imagRes.StatusCode)
		picture, err := io.ReadAll(imagRes.Body)
		require.NoError(t, err)

		out, err := provider.Execute(ctx, &types.Request{
			Config: config,
			History: []types.Message{
				{
					Role: types.RoleUser,
					User: "reddec",
					Content: types.Content{
						Data: picture,
						Mime: types.MIMEJpg,
					},
				},
				userMessage("reddec", "Describe the picture"),
			},
			Tools: &tools,
		})
		require.NoError(t, err)
		require.Len(t, out.Messages, 1)

		for _, msg := range out.Messages {
			t.Logf("input: %s", string(msg.Input))
			t.Logf("output: %s", string(msg.Output))
			require.Len(t, msg.Content, 1)
			for _, content := range msg.Content {
				t.Logf("%s: %s", content.Mime, string(content.Data))
			}
		}
		reply := out.Reply()

		t.Logf("%s", reply)
		require.Contains(t, strings.ToLower(string(reply.Data)), "eiffel")
	})
}
