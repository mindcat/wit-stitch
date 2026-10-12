package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Getter fetches a URL's body. Non-2xx responses must be returned as errors.
type Getter interface {
	Get(ctx context.Context, url string) ([]byte, error)
}

// Resolve works out which tiled image rawURL refers to. It accepts a tile URL, a
// metadata URL (ImageProperties.xml, .dzi, info.json), a Zoomify folder, or a web
// page that embeds one of those.
func Resolve(ctx context.Context, g Getter, rawURL string) (*Image, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("not an http(s) URL: %q", rawURL)
	}
	u.RawQuery, u.Fragment = "", ""
	clean := u.String()

	if img, ok, err := fromURLShape(ctx, g, clean); ok {
		return img, err
	}

	body, err := g.Get(ctx, rawURL)
	if err == nil {
		if img, err := fromBody(clean, body); img != nil || err != nil {
			return img, err
		}
	}
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}

	// A folder URL: try the metadata files that would live inside it.
	dir := strings.TrimRight(clean, "/")
	for _, cand := range []string{dir + "/ImageProperties.xml", dir + "/info.json"} {
		if img, err := load(ctx, g, cand); err == nil {
			return img, nil
		}
	}

	// A web page: look for references to tiled images in its source.
	if body != nil {
		for _, cand := range pageCandidates(u, body) {
			if img, err := tryCandidate(ctx, g, cand); err == nil {
				return img, nil
			} else {
				errs = append(errs, err)
			}
		}
	}
	errs = append([]error{errors.New("could not find a Zoomify, Deep Zoom or IIIF image at " + rawURL)}, errs...)
	return nil, errors.Join(errs...)
}

// fromURLShape recognises URLs whose path alone says which format they are.
func fromURLShape(ctx context.Context, g Getter, u string) (*Image, bool, error) {
	if base, ok := zoomifyBase(u); ok {
		img, err := load(ctx, g, base+"/ImageProperties.xml")
		return img, true, err
	}
	if info, ok := iiifInfoURL(u); ok {
		img, err := load(ctx, g, info)
		return img, true, err
	}
	if cands := dziMetaCandidates(u); cands != nil {
		var errs []error
		for _, c := range cands {
			img, err := load(ctx, g, c)
			if err == nil {
				return img, true, nil
			}
			errs = append(errs, err)
		}
		return nil, true, errors.Join(errs...)
	}
	return nil, false, nil
}

// load fetches a metadata document and parses it according to its content.
func load(ctx context.Context, g Getter, metaURL string) (*Image, error) {
	body, err := g.Get(ctx, metaURL)
	if err != nil {
		return nil, err
	}
	img, err := fromBody(metaURL, body)
	if img == nil && err == nil {
		err = fmt.Errorf("%s: not a recognised tile descriptor", metaURL)
	}
	return img, err
}

// fromBody sniffs a metadata document. It returns (nil, nil) if the body is none of
// the known formats.
func fromBody(metaURL string, body []byte) (*Image, error) {
	switch {
	case isZoomifyProps(body):
		base := metaURL
		if m := zoomifyPropsRe.FindStringSubmatch(metaURL); m != nil {
			base = m[1]
		}
		return parseZoomify(strings.TrimRight(base, "/"), body)
	case isIIIF(body):
		return parseIIIF(metaURL, body)
	case isDZI(body):
		return parseDZI(metaURL, body)
	}
	return nil, nil
}

func tryCandidate(ctx context.Context, g Getter, cand string) (*Image, error) {
	if img, ok, err := fromURLShape(ctx, g, cand); ok {
		return img, err
	}
	// Bare folder references, e.g. a Zoomify viewer configured with an image path.
	return load(ctx, g, strings.TrimRight(cand, "/")+"/ImageProperties.xml")
}

var (
	quotedRe = regexp.MustCompile(`["'(]([^"'()\s<>]{4,})["')]`)
	hintRe   = regexp.MustCompile(`(?i)ImageProperties\.xml|\.dzi\b|/info\.json|/TileGroup\d+/|_files/\d+/|zoomify`)
)

// pageCandidates pulls strings out of an HTML/JS page that look like references to
// tiled images, resolved against the page URL, most specific first.
func pageCandidates(page *url.URL, body []byte) []string {
	src := strings.ReplaceAll(string(body), `\/`, `/`)
	seen := map[string]bool{}
	var direct, folders []string
	for _, m := range quotedRe.FindAllStringSubmatch(src, -1) {
		s := m[1]
		if !hintRe.MatchString(s) {
			continue
		}
		ref, err := url.Parse(s)
		if err != nil {
			continue
		}
		abs := page.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			continue
		}
		abs.RawQuery, abs.Fragment = "", ""
		a := abs.String()
		if seen[a] {
			continue
		}
		seen[a] = true
		if _, ok := zoomifyBase(a); ok || dziMetaCandidates(a) != nil {
			direct = append(direct, a)
		} else if _, ok := iiifInfoURL(a); ok {
			direct = append(direct, a)
		} else if extOf(a) == "" {
			folders = append(folders, a)
		}
	}
	out := append(direct, folders...)
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}
