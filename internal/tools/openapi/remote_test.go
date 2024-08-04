package openapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pikocloud/pikobrain/testutils"

	"github.com/pikocloud/pikobrain/internal/providers/openai"
	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/tools/openapi"
)

func TestWithAI(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts, err := openapi.New(ctx, openapi.Config{
		URL:                     "https://petstore3.swagger.io/api/v3/openapi.json",
		Timeout:                 5 * time.Second,
		MaxResponse:             1024 * 1024,
		IgnoreInvalidOperations: true,
		AcceptJSON:              true,
	})
	require.NoError(t, err)

	chatGPT := openai.New("https://api.openai.com/v1", os.Getenv("OPENAI_TOKEN"))

	var config = types.Config{
		Model:         "gpt-4o-mini",
		Prompt:        "Your are the helpful assistant",
		MaxTokens:     300,
		MaxIterations: 2,
	}

	var toolbox types.DynamicToolbox
	for _, tool := range ts {
		toolbox.Add(tool)
	}

	err = toolbox.Update(ctx, false)
	require.NoError(t, err)

	out, err := chatGPT.Execute(context.TODO(), &types.Request{
		Config: config,
		History: []types.Message{
			{
				Role: types.RoleUser,
				User: "reddec",
				Content: types.Content{
					Mime: types.MIMEText,
					Data: []byte("Which is under ID 9?"),
				},
			},
		},
		Tools: &toolbox,
	})
	require.NoError(t, err)
	//require.Len(t, out.Messages, 1)

	for _, msg := range out.Messages {
		t.Logf("input: %s", string(msg.Input))
		t.Logf("output: %s", string(msg.Output))
		//require.Len(t, msg.bodyContent, 1)
		for _, content := range msg.Content {
			t.Logf("%s: %s", content.Mime, string(content.Data))
		}
	}

	require.Contains(t, string(out.Reply().Data), getActualName(ctx, 9))
}

func TestMain(m *testing.M) {
	testutils.LoadEnv()
	testutils.SetupLogging()
	os.Exit(m.Run())
}

func getActualName(ctx context.Context, id int) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://petstore3.swagger.io/api/v3/pet/"+strconv.Itoa(id), nil)
	if err != nil {
		panic(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()

	var out struct {
		Name string `json:"name"`
	}

	err = json.NewDecoder(res.Body).Decode(&out)
	if err != nil {
		panic(err)
	}
	return out.Name
}
