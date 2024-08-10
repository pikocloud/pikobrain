package web

import (
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/Masterminds/sprig/v3"
	"github.com/reddec/view"

	"github.com/pikocloud/pikobrain/internal/brain"
	"github.com/pikocloud/pikobrain/internal/ent"
	"github.com/pikocloud/pikobrain/internal/ent/message"
)

func New(db *ent.Client, brain *brain.Brain, baseURL string) (*Web, error) {
	srv := &Web{
		db:      db,
		brain:   brain,
		baseURL: baseURL,
	}
	funcs := sprig.HtmlFuncMap()
	funcs["url"] = func(v string) template.URL {
		return template.URL(v)
	}
	funcs["bytesToString"] = func(v []byte) string {
		return string(v)
	}
	root := template.New("").Funcs(funcs)

	var err error

	srv.viewIndex, err = view.NewTemplate[indexView](template.Must(root.Clone()), views, "views/index.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse index view: %w", err)
	}

	srv.viewThreads, err = view.NewTemplate[threadsView](template.Must(root.Clone()), views, "views/threads.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse threads view: %w", err)
	}

	srv.viewThread, err = view.NewTemplate[threadView](template.Must(root.Clone()), views, "views/thread.gohtml")
	if err != nil {
		return nil, fmt.Errorf("parse thread view: %w", err)
	}

	return srv, nil
}

type Web struct {
	db      *ent.Client
	brain   *brain.Brain
	baseURL string

	viewIndex   *view.View[indexView]
	viewThreads *view.View[threadsView]
	viewThread  *view.View[threadView]
}

func (w *Web) Index(res http.ResponseWriter, req *http.Request) {
	const lastLimit = 10
	ctx := req.Context()

	messages, err := w.db.Message.Query().Order(message.ByID(sql.OrderDesc())).Limit(lastLimit).All(ctx)
	if err != nil {
		_ = w.viewIndex.Render(res, indexView{baseView: w.withError("Get last messages", err)})
		return
	}

	err = w.viewIndex.Render(res, indexView{
		baseView:     w.base(),
		Definition:   w.brain.Definition(),
		LastMessages: messages,
	})
	if err != nil {
		slog.Error("UI page (index) render failed", "error", err)
	}
}

func (w *Web) Threads(res http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	var records []threadMeta
	err := w.db.Message.Query().Select(message.FieldThread).GroupBy(message.FieldThread).Aggregate(ent.Count()).Scan(ctx, &records)
	if err != nil {
		_ = w.viewThreads.Render(res, threadsView{baseView: w.withError("Get threads", err)})
		return
	}
	slices.SortFunc(records, func(a, b threadMeta) int {
		return strings.Compare(a.Thread, b.Thread)
	})

	err = w.viewThreads.Render(res, threadsView{
		baseView: w.base(),
		Threads:  records,
	})
	if err != nil {
		slog.Error("UI page (index) render failed", "error", err)
	}
}

func (w *Web) Thread(res http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	thread := req.PathValue("thread")
	offset := parseInt(req.FormValue("offset"), 0, math.MaxInt, 0)
	limit := parseInt(req.FormValue("limit"), 1, 50, 25)

	items, err := w.db.Message.Query().Where(message.Thread(thread)).Order(message.ByID(sql.OrderAsc())).Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		_ = w.viewThread.Render(res, threadView{baseView: w.withError("Get messages", err)})
		return
	}

	num, err := w.db.Message.Query().Where(message.Thread(thread)).Count(ctx)
	if err != nil {
		_ = w.viewThread.Render(res, threadView{baseView: w.withError("Get total amount", err)})
		return
	}

	err = w.viewThread.Render(res, threadView{
		baseView: w.base(),
		pagination: pagination{
			Offset:      offset,
			Limit:       limit,
			Total:       num,
			Pages:       (num + limit + 1) / limit, // ceil
			CurrentPage: (offset + limit + 1) / limit,
		},
		Thread:   thread,
		Messages: items,
	})
	if err != nil {
		slog.Error("UI page (view thread) render failed", "error", err)
	}
}

func (w *Web) base() baseView {
	return baseView{BaseURL: w.baseURL}
}

func (w *Web) withError(title string, err any) baseView {
	return baseView{
		BaseURL: w.baseURL,
		Notifications: []notification{
			{
				Kind:    KindError,
				Title:   title,
				Message: fmt.Sprint(err),
			},
		},
	}
}

func parseInt(value string, minValue, maxValue, defaultValue int) int {
	v, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return max(min(v, maxValue), minValue)
}
