// Package wishlist reads product pages for the wishlist (title, price, image) and ranks items
// against the money saved in the Wishlist goal.
package wishlist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"

	"viceroy/internal/money"
)

const (
	maxPageBytes  = 2 << 20
	maxImageBytes = 5 << 20
	fetchTimeout  = 6 * time.Second
	maxRedirects  = 3
	userAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36"
)

// Preview is what a product page says about the product. Anything not found is empty.
type Preview struct {
	URL      string `json:"url"`   // the page after redirects, without tracking parameters
	Store    string `json:"store"` // e.g. amazon.com
	Title    string `json:"title"`
	Price    *int64 `json:"price_cents"`
	ImageURL string `json:"image_url"`
	// Blocked is set when the store refused the request (a captcha or an error page) or no
	// price could be read; the user fills in what's missing.
	Blocked bool `json:"blocked"`
	// Text is the page's visible text (capped), for the AI fallback. Not sent to the client.
	Text string `json:"-"`
}

// ErrBadURL is returned for links that aren't http(s) or point at a private address.
var ErrBadURL = errors.New("enter a link starting with http:// or https://")

// errPrivate is the dialer's refusal of loopback/private/link-local addresses.
var errPrivate = errors.New("that link points to a private network address")

// Fetcher downloads pages and images from the internet, refusing private addresses so a link
// can't reach services on the server's network.
type Fetcher struct {
	// AllowPrivate turns the private-address guard off (tests and the e2e fake store only).
	AllowPrivate bool
	client       *http.Client
}

func (f *Fetcher) httpClient() *http.Client {
	if f.client != nil {
		return f.client
	}
	dialer := &net.Dialer{Timeout: fetchTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		if f.AllowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil || blockedIP(ip) {
			return errPrivate
		}
		return nil
	}}
	f.client = &http.Client{
		Timeout: fetchTimeout,
		Transport: &http.Transport{
			Proxy:                 nil, // a proxy would dial for us and skip the address check
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   fetchTimeout,
			ResponseHeaderTimeout: fetchTimeout,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return ErrBadURL
			}
			return nil
		},
	}
	return f.client
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

func blockedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		cgnat.Contains(ip) || (ip.Is4() && ip.As4()[0] == 0)
}

// CheckURL parses a user-entered link (a missing scheme means https).
func CheckURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, ErrBadURL
	}
	return u, nil
}

func (f *Fetcher) get(ctx context.Context, u string, accept string, limit int64) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, ErrBadURL
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := f.httpClient().Do(req)
	if err != nil {
		if errors.Is(err, errPrivate) {
			return nil, nil, errPrivate
		}
		return nil, nil, fmt.Errorf("couldn't open the link: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't read the page: %w", err)
	}
	return resp, body, nil
}

// Fetch downloads a product page and reads it. A page that loads but can't be read still
// returns a Preview (with Blocked set) so the user can fill in the rest.
func (f *Fetcher) Fetch(ctx context.Context, raw string) (Preview, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return Preview{}, err
	}
	resp, body, err := f.get(ctx, u.String(), "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8", maxPageBytes)
	if err != nil {
		return Preview{}, err
	}
	p := Parse(resp.Request.URL, body)
	if resp.StatusCode >= 400 {
		p.Blocked = true
	}
	return p, nil
}

// Image downloads an image (at most 5 MB) and returns its bytes and sniffed type.
func (f *Fetcher) Image(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return nil, "", err
	}
	resp, body, err := f.get(ctx, u.String(), "image/*", maxImageBytes)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("the image didn't load (%s)", resp.Status)
	}
	return body, http.DetectContentType(body), nil
}

// ---- parsing ----

// Parse reads a product page: JSON-LD Product data, then OpenGraph/product meta tags,
// itemprop="price", then store-specific spots (Amazon, eBay, Target).
func Parse(page *url.URL, body []byte) Preview {
	p := Preview{URL: CleanURL(page).String(), Store: StoreName(page.Hostname())}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		p.Blocked = true
		return p
	}
	var (
		title, ogTitle, ogImage, metaPrice, itempropPrice string
		amazonPrice, amazonTitle, amazonImage             string
		ebayPrice, ebayTitle, targetPrice                 string
		ldTitle, ldImage, ldPrice                         string
		text                                              strings.Builder
		captcha                                           bool
	)
	var walk func(n *html.Node, skip bool)
	walk = func(n *html.Node, skip bool) {
		if n.Type == html.TextNode && !skip {
			if t := strings.Join(strings.Fields(n.Data), " "); t != "" && text.Len() < 8000 {
				text.WriteString(t)
				text.WriteByte('\n')
			}
		}
		if n.Type == html.ElementNode {
			id, class := attr(n, "id"), " "+attr(n, "class")+" "
			switch n.Data {
			case "script", "style", "noscript", "template":
				skip = true
				if n.Data == "script" {
					src := nodeText(n)
					switch {
					case attr(n, "type") == "application/ld+json":
						t, img, pr := readJSONLD(src)
						ldTitle, ldImage, ldPrice = first(ldTitle, t), first(ldImage, img), first(ldPrice, pr)
					case strings.Contains(src, "__TGT_DATA__") || strings.Contains(src, "current_retail"):
						if m := targetRetail.FindStringSubmatch(src); m != nil {
							targetPrice = first(targetPrice, m[1])
						}
					}
				}
			case "title":
				title = first(title, strings.TrimSpace(nodeText(n)))
			case "meta":
				key := strings.ToLower(first(attr(n, "property"), attr(n, "name")))
				content := strings.TrimSpace(attr(n, "content"))
				switch key {
				case "og:title", "twitter:title":
					ogTitle = first(ogTitle, content)
				case "og:image", "og:image:secure_url", "twitter:image":
					ogImage = first(ogImage, content)
				case "product:price:amount", "og:price:amount":
					metaPrice = first(metaPrice, content)
				}
				if attr(n, "itemprop") == "price" {
					itempropPrice = first(itempropPrice, content)
				}
			case "form":
				if strings.Contains(attr(n, "action"), "validateCaptcha") {
					captcha = true
				}
			case "img":
				if id == "landingImage" || id == "imgBlkFront" {
					amazonImage = first(amazonImage, attr(n, "data-old-hires"), attr(n, "src"))
				}
			}
			if n.Data != "meta" && attr(n, "itemprop") == "price" {
				itempropPrice = first(itempropPrice, attr(n, "content"), strings.TrimSpace(nodeText(n)))
			}
			switch {
			case id == "productTitle":
				amazonTitle = first(amazonTitle, strings.TrimSpace(nodeText(n)))
			case id == "corePrice_feature_div" || id == "corePriceDisplay_desktop_feature_div" || id == "apex_desktop":
				if off := findClass(n, "a-offscreen"); off != nil {
					amazonPrice = first(amazonPrice, strings.TrimSpace(nodeText(off)))
				}
			case id == "priceblock_ourprice" || id == "priceblock_dealprice":
				amazonPrice = first(amazonPrice, strings.TrimSpace(nodeText(n)))
			case strings.Contains(class, " x-price-primary "):
				ebayPrice = first(ebayPrice, strings.TrimSpace(nodeText(n)))
			case strings.Contains(class, " x-item-title__mainTitle "):
				ebayTitle = first(ebayTitle, strings.TrimSpace(nodeText(n)))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, skip)
		}
	}
	walk(doc, false)

	p.Title = cleanTitle(first(ldTitle, amazonTitle, ebayTitle, ogTitle, title), p.Store)
	p.ImageURL = resolve(page, first(ldImage, amazonImage, ogImage))
	for _, s := range []string{ldPrice, metaPrice, itempropPrice, amazonPrice, ebayPrice, targetPrice} {
		if c, ok := ParsePrice(s); ok {
			p.Price = &c
			break
		}
	}
	p.Text = text.String()
	lower := strings.ToLower(p.Text)
	if captcha || strings.Contains(lower, "enter the characters you see below") || strings.EqualFold(title, "Robot Check") {
		p.Blocked = true
	}
	if p.Price == nil {
		p.Blocked = true
	}
	return p
}

var targetRetail = regexp.MustCompile(`\\?"current_retail\\?"\s*:\s*([0-9]+(?:\.[0-9]{1,2})?)`)

// readJSONLD finds the first Product in a JSON-LD block (top level, an array, or @graph).
func readJSONLD(src string) (title, image, price string) {
	var v any
	if json.Unmarshal([]byte(strings.TrimSpace(src)), &v) != nil {
		return
	}
	var prod map[string]any
	var find func(v any)
	find = func(v any) {
		if prod != nil {
			return
		}
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				find(e)
			}
		case map[string]any:
			if isType(t["@type"], "Product") {
				prod = t
				return
			}
			find(t["@graph"])
			find(t["mainEntity"])
		}
	}
	find(v)
	if prod == nil {
		return
	}
	title, _ = prod["name"].(string)
	image = imageURL(prod["image"])
	price = offerPrice(prod["offers"])
	return strings.TrimSpace(html.UnescapeString(title)), image, price
}

func isType(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return strings.EqualFold(t, want) || strings.HasSuffix(t, "/"+want)
	case []any:
		for _, e := range t {
			if isType(e, want) {
				return true
			}
		}
	}
	return false
}

func imageURL(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		for _, e := range t {
			if s := imageURL(e); s != "" {
				return s
			}
		}
	case map[string]any:
		if s, ok := t["url"].(string); ok {
			return s
		}
		if s, ok := t["contentUrl"].(string); ok {
			return s
		}
	}
	return ""
}

func offerPrice(v any) string {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			if s := offerPrice(e); s != "" {
				return s
			}
		}
	case map[string]any:
		if cur, ok := t["priceCurrency"].(string); ok && cur != "" && !strings.EqualFold(cur, "USD") {
			return ""
		}
		for _, k := range []string{"price", "lowPrice"} {
			switch p := t[k].(type) {
			case string:
				if p != "" {
					return p
				}
			case float64:
				return fmt.Sprintf("%.2f", p)
			}
		}
		if s := offerPrice(t["priceSpecification"]); s != "" {
			return s
		}
		return offerPrice(t["offers"])
	}
	return ""
}

var priceNumber = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]{1,2})?`)

// ParsePrice reads "$1,299.99", "1299.99" or "USD 25" as cents. Zero isn't a price.
func ParsePrice(s string) (int64, bool) {
	m := priceNumber.FindString(s)
	if m == "" {
		return 0, false
	}
	c, err := money.ParseCents(m)
	if err != nil || c <= 0 || c > 100_000_000_00 {
		return 0, false
	}
	return c, true
}

// ---- helpers ----

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func findClass(n *html.Node, class string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && strings.Contains(" "+attr(c, "class")+" ", " "+class+" ") {
			return c
		}
		if f := findClass(c, class); f != nil {
			return f
		}
	}
	return nil
}

func first(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func resolve(base *url.URL, ref string) string {
	if ref == "" {
		return ""
	}
	u, err := base.Parse(strings.TrimSpace(ref))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.String()
}

// cleanTitle drops store boilerplate such as "Amazon.com: " and " - eBay".
func cleanTitle(t, store string) string {
	t = strings.Join(strings.Fields(t), " ")
	for _, p := range []string{"Amazon.com: ", "Amazon.com : "} {
		t = strings.TrimPrefix(t, p)
	}
	for _, sfx := range []string{" : Amazon.com", " | eBay", " - eBay", " : Target", " - Target"} {
		t = strings.TrimSuffix(t, sfx)
	}
	if i := strings.LastIndex(t, " : "); i > 0 && strings.HasPrefix(store, "amazon.") {
		t = t[:i] // "Title : Electronics"
	}
	if len(t) > 200 {
		t = t[:200]
	}
	return t
}

// StoreName is the host without "www." and similar prefixes.
func StoreName(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, p := range []string{"www.", "smile.", "m.", "shop."} {
		host = strings.TrimPrefix(host, p)
	}
	return host
}

var (
	amazonASIN = regexp.MustCompile(`/(?:dp|gp/product|gp/aw/d)/([A-Z0-9]{10})`)
	tracking   = regexp.MustCompile(`^(utm_.*|ref|ref_|tag|psc|pd_rd_.*|pf_rd_.*|_encoding|qid|sr|keywords|crid|sprefix|dib|dib_tag|th|linkcode|linkid|content-id|smid|spla|sp_csd|gclid|fbclid|msclkid|mc_cid|mc_eid|_trkparms|_trksid|hash|amdata|mkcid|mkevt|mkrid|campid|toolid|customid|afsrc|clickid|irgwc|cjevent|srsltid)$`)
)

// CleanURL drops tracking parameters, and turns Amazon product links into /dp/<ASIN>.
func CleanURL(u *url.URL) *url.URL {
	c := *u
	c.Fragment = ""
	if strings.Contains(c.Hostname(), "amazon.") {
		if m := amazonASIN.FindStringSubmatch(c.Path); m != nil {
			c.Path, c.RawPath, c.RawQuery = "/dp/"+m[1], "", ""
			return &c
		}
	}
	q := c.Query()
	for k := range q {
		if tracking.MatchString(strings.ToLower(k)) {
			q.Del(k)
		}
	}
	c.RawQuery = q.Encode()
	return &c
}
