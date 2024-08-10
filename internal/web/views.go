package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"

	"github.com/pikocloud/pikobrain/internal/brain"
	"github.com/pikocloud/pikobrain/internal/ent"
)

//go:embed all:views
var views embed.FS

type Kind string

const (
	KindInfo  Kind = "info"
	KindError Kind = "error"
)

type notification struct {
	Kind    Kind
	Title   string
	Message string
}

type baseView struct {
	BaseURL       string
	Notifications []notification
}

func (b baseView) URL(link string, params ...any) (template.URL, error) {
	u, err := url.Parse(b.BaseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	rel, err := url.Parse(link)
	if err != nil {
		return "", fmt.Errorf("parse link: %w", err)
	}
	next := u.ResolveReference(rel)

	if len(params) > 1 {
		q := next.Query()
		for i := range len(params) / 2 {
			q.Add(fmt.Sprint(params[i*2]), fmt.Sprint(params[(i*2)+1]))
		}
		next.RawQuery = q.Encode()
	}

	return template.URL(next.String()), nil
}

type indexView struct {
	baseView
	Definition   brain.Definition
	LastMessages []*ent.Message
}

type threadsView struct {
	baseView
	Threads []threadMeta
}

type threadMeta struct {
	Thread string
	Count  int
}

type pagination struct {
	Offset      int
	Limit       int
	Total       int
	Pages       int
	CurrentPage int
}

type threadView struct {
	baseView
	pagination
	Thread   string
	Messages []*ent.Message
}

func (tv pagination) HasNext() bool {
	return tv.Offset+tv.Limit < tv.Total
}

func (tv pagination) HasPrev() bool {
	return tv.Offset > 0
}

func (tv pagination) PrevOffset() int {
	return tv.Offset - tv.Limit
}

func (tv pagination) NextOffset() int {
	return tv.Offset + tv.Limit
}

func (tv pagination) GenPages() []Page {
	// 1 ... (prev), Current, (next) ... N
	var ans []Page
	if tv.CurrentPage > 2 {
		// first page
		ans = append(ans, Page{
			Offset: 0,
			Limit:  tv.Limit,
		})
	}

	if tv.CurrentPage > 3 {
		// filler
		ans = append(ans, Page{
			Empty: true,
		})
	}

	if tv.HasPrev() {
		ans = append(ans, Page{
			Offset: tv.PrevOffset(),
			Limit:  tv.Limit,
		})
	}
	ans = append(ans, Page{
		Offset: tv.Offset,
		Limit:  tv.Limit,
	})
	if tv.HasNext() {
		ans = append(ans, Page{
			Offset: tv.NextOffset(),
			Limit:  tv.Limit,
		})
	}
	if tv.CurrentPage < tv.Pages-3 {
		// filler
		ans = append(ans, Page{
			Empty: true,
		})
	}

	if tv.CurrentPage < tv.Pages-2 {
		// last page
		ans = append(ans, Page{
			Offset: tv.Limit * (tv.Pages - 2),
			Limit:  tv.Limit,
		})
	}
	return ans
}

type Page struct {
	Offset int
	Limit  int
	Empty  bool
}

func (p Page) Page() int { return (p.Offset + p.Limit + 1) / p.Limit }

//go:embed all:static
var static embed.FS

func MustStatic() fs.FS {
	v, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}
	return v
}
