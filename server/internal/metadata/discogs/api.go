package discogs

import (
	"cmp"
	"context"
	"net/url"
	"strconv"
	"strings"
)

type Track struct {
	Position string `json:"position"`
	Type     string `json:"type_"`
	Title    string `json:"title"`
	Duration string `json:"duration"`
}

type Image struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

type Master struct {
	ID        int64    `json:"id"`
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Artists   []Artist `json:"artists"`
	Genres    []string `json:"genres"`
	Styles    []string `json:"styles"`
	Tracklist []Track  `json:"tracklist"`
	Images    []Image  `json:"images"`
}

type Label struct {
	Name  string `json:"name"`
	CatNo string `json:"catno"`
}

type Format struct {
	Name         string   `json:"name"`
	Descriptions []string `json:"descriptions"`
}

type Release struct {
	ID        int64    `json:"id"`
	MasterID  int64    `json:"master_id"`
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Country   string   `json:"country"`
	Artists   []Artist `json:"artists"`
	Genres    []string `json:"genres"`
	Styles    []string `json:"styles"`
	Labels    []Label  `json:"labels"`
	Formats   []Format `json:"formats"`
	Tracklist []Track  `json:"tracklist"`
	Images    []Image  `json:"images"`
}

func (r Release) FormatText() string {
	var parts []string
	for _, f := range r.Formats {
		parts = append(parts, strings.Join(append([]string{f.Name}, f.Descriptions...), ", "))
	}
	return strings.Join(parts, "; ")
}

// PrimaryImage returns the image Discogs marks as primary, falling back to the first one.
func PrimaryImage(images []Image) string {
	for _, img := range images {
		if img.Type == "primary" {
			return img.URI
		}
	}
	if len(images) > 0 {
		return images[0].URI
	}
	return ""
}

func (c *Client) Master(ctx context.Context, id int64) (Master, error) {
	return byID[Master](ctx, c, "/masters/", id)
}

func (c *Client) Release(ctx context.Context, id int64) (Release, error) {
	return byID[Release](ctx, c, "/releases/", id)
}

func byID[T any](ctx context.Context, c *Client, path string, id int64) (T, error) {
	var v T
	err := c.get(ctx, path+strconv.FormatInt(id, 10), nil, &v)
	return v, err
}

type Result struct {
	Kind    string   `json:"kind"`
	ID      int64    `json:"id"`
	Artist  string   `json:"artist"`
	Title   string   `json:"title"`
	Year    string   `json:"year,omitempty"`
	Country string   `json:"country,omitempty"`
	Format  []string `json:"format,omitempty"`
	Thumb   string   `json:"thumb,omitempty"`
	CatNo   string   `json:"catno,omitempty"`
}

type rawResult struct {
	Type     string   `json:"type"`
	ID       int64    `json:"id"`
	MasterID int64    `json:"master_id"`
	Title    string   `json:"title"`
	Year     string   `json:"year"`
	Country  string   `json:"country"`
	Format   []string `json:"format"`
	Thumb    string   `json:"thumb"`
	Cover    string   `json:"cover_image"`
	CatNo    string   `json:"catno"`
}

// Search finds albums. Releases that belong to a master are folded into that master so each album shows once;
// releases without a master are returned as standalone entries.
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	var resp struct {
		Results []rawResult `json:"results"`
	}
	q := url.Values{"q": {query}, "per_page": {"30"}}
	if err := c.get(ctx, "/database/search", q, &resp); err != nil {
		return nil, err
	}
	out := []Result{}
	type key struct {
		kind string
		id   int64
	}
	seen := map[key]bool{}
	for _, r := range resp.Results {
		res, ok := r.result()
		if k := (key{res.Kind, res.ID}); ok && !seen[k] {
			seen[k] = true
			out = append(out, res)
		}
	}
	return out, nil
}

// result turns a search hit into a pick: a pressing that belongs to a master stands for the master.
func (r rawResult) result() (Result, bool) {
	artist, title := splitTitle(r.Title)
	res := Result{Kind: "master", ID: r.ID, Artist: artist, Title: title, Year: r.Year, Thumb: cmp.Or(r.Thumb, r.Cover)}
	switch {
	case r.Type == "master":
	case r.Type == "release" && r.MasterID != 0:
		res.ID = r.MasterID
	case r.Type == "release":
		res.Kind = "release"
		res.Country, res.Format, res.CatNo = r.Country, r.Format, r.CatNo
	default:
		return Result{}, false
	}
	return res, true
}

type VersionFilter struct {
	Country  string
	Year     string
	Label    string
	CatNo    string
	AnyMedia bool
}

type Version struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Label    string `json:"label"`
	CatNo    string `json:"catno"`
	Country  string `json:"country"`
	Released string `json:"released"`
	Format   string `json:"format"`
	Thumb    string `json:"thumb"`
}

func (c *Client) Versions(ctx context.Context, masterID int64, f VersionFilter) ([]Version, error) {
	q := url.Values{"per_page": {"100"}, "sort": {"released"}}
	if !f.AnyMedia {
		q.Set("format", "Vinyl")
	}
	if f.Country != "" {
		q.Set("country", f.Country)
	}
	if f.Year != "" {
		q.Set("released", f.Year)
	}
	if f.Label != "" {
		q.Set("label", f.Label)
	}
	var resp struct {
		Versions []Version `json:"versions"`
	}
	if err := c.get(ctx, "/masters/"+strconv.FormatInt(masterID, 10)+"/versions", q, &resp); err != nil {
		return nil, err
	}
	if f.CatNo == "" {
		return resp.Versions, nil
	}
	want := normalizeCatNo(f.CatNo)
	out := []Version{}
	for _, v := range resp.Versions {
		if strings.Contains(normalizeCatNo(v.CatNo), want) {
			out = append(out, v)
		}
	}
	return out, nil
}

func normalizeCatNo(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '.' {
			return -1
		}
		return r
	}, strings.ToUpper(s))
}

func splitTitle(s string) (artist, title string) {
	if a, t, ok := strings.Cut(s, " - "); ok {
		return CleanName(a), strings.TrimSpace(t)
	}
	return "", strings.TrimSpace(s)
}
