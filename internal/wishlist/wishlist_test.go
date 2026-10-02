package wishlist

import (
	"bytes"
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"

	"viceroy/internal/ai"
	"viceroy/internal/ai/fakeai"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func cents(c int64) *int64 { return &c }

func TestParse(t *testing.T) {
	cases := []struct {
		file, page     string
		title, image   string
		price          *int64
		blocked        bool
		store, cleaned string
	}{
		{"jsonld.html", "https://www.brightside.example/p/lamp?utm_source=mail&color=brass", "Brightside Reading Lamp", "https://www.brightside.example/shop/lamp.png", cents(4999), false,
			"brightside.example", "https://www.brightside.example/p/lamp?color=brass"},
		{"og.html", "https://panco.example/skillet", "Cast Iron Skillet, 12 inch", "https://cdn.panco.example/skillet.jpg", cents(3450), false, "panco.example", "https://panco.example/skillet"},
		{"amazon.html", "https://www.amazon.com/Noise-Cancelling/dp/B0ABCDEFGH/ref=sr_1_3?keywords=x&qid=1", "Noise Cancelling Headphones, Wireless",
			"https://m.media-amazon.example/images/I/large.jpg", cents(119900), false, "amazon.com", "https://www.amazon.com/dp/B0ABCDEFGH"},
		{"ebay.html", "https://www.ebay.com/itm/1234?_trkparms=abc&hash=item1", "Vintage Film Camera 35mm", "https://i.ebayimg.example/images/g/camera/s-l1600.jpg", cents(8500), false, "ebay.com", "https://www.ebay.com/itm/1234"},
		{"target.html", "https://www.target.com/p/mixer/-/A-1", "Stand Mixer", "https://target.scene7.example/is/image/Target/mixer", cents(27999), false, "target.com", "https://www.target.com/p/mixer/-/A-1"},
		{"captcha.html", "https://www.amazon.com/dp/B0ABCDEFGH", "Robot Check", "", nil, true, "amazon.com", "https://www.amazon.com/dp/B0ABCDEFGH"},
		{"textonly.html", "https://woodworks.example/bench", "Garden Bench - Woodworks", "", nil, true, "woodworks.example", "https://woodworks.example/bench"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			u, _ := url.Parse(c.page)
			p := Parse(u, fixture(t, c.file))
			if p.Title != c.title || p.ImageURL != c.image || p.Blocked != c.blocked || p.Store != c.store || p.URL != c.cleaned {
				t.Errorf("got title=%q image=%q blocked=%v store=%q url=%q", p.Title, p.ImageURL, p.Blocked, p.Store, p.URL)
			}
			if (p.Price == nil) != (c.price == nil) || (p.Price != nil && *p.Price != *c.price) {
				t.Errorf("price = %v, want %v", p.Price, c.price)
			}
		})
	}
}

func TestParsePrice(t *testing.T) {
	for in, want := range map[string]int64{"$1,299.99": 129999, "US $85.00": 8500, "25": 2500, "USD 7.5": 750} {
		if got, ok := ParsePrice(in); !ok || got != want {
			t.Errorf("ParsePrice(%q) = %d %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "free", "$0.00"} {
		if _, ok := ParsePrice(in); ok {
			t.Errorf("ParsePrice(%q) accepted", in)
		}
	}
}

func TestBlockedIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.9", "172.16.0.1", "169.254.169.254", "100.64.1.1", "0.0.0.0", "::1", "fe80::1", "fd00::1", "::ffff:127.0.0.1"} {
		if !blockedIP(netip.MustParseAddr(s)) {
			t.Errorf("%s not blocked", s)
		}
	}
	for _, s := range []string{"93.184.216.34", "2606:4700::1111"} {
		if blockedIP(netip.MustParseAddr(s)) {
			t.Errorf("%s blocked", s)
		}
	}
}

func TestFetchGuard(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "/p", http.StatusFound)
			return
		}
		w.Write(fixture(t, "jsonld.html"))
	}))
	defer ts.Close()
	ctx := context.Background()

	// The test server is on 127.0.0.1: refused, also when reached through a redirect from a
	// "public" host (here the guard sees every connection, so a redirect can't get around it).
	var f Fetcher
	if _, err := f.Fetch(ctx, ts.URL+"/p"); err != errPrivate {
		t.Fatalf("private fetch err = %v", err)
	}
	if _, err := f.Fetch(ctx, "file:///etc/passwd"); err != ErrBadURL {
		t.Fatalf("file url err = %v", err)
	}
	if _, err := f.Fetch(ctx, "ftp://example.com/x"); err != ErrBadURL {
		t.Fatalf("ftp url err = %v", err)
	}

	open := Fetcher{AllowPrivate: true}
	p, err := open.Fetch(ctx, ts.URL+"/hop?utm_campaign=x")
	if err != nil || p.Title != "Brightside Reading Lamp" || p.URL != ts.URL+"/p" || p.Price == nil {
		t.Fatalf("fetch = %+v, %v", p, err)
	}
}

func TestRedirectToPrivateRefused(t *testing.T) {
	// A redirect target is dialed through the same guard.
	priv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("secret")) }))
	defer priv.Close()
	var f Fetcher
	c := f.httpClient()
	req, _ := http.NewRequest("GET", priv.URL, nil)
	if _, err := c.Do(req); err == nil || !strings.Contains(err.Error(), errPrivate.Error()) {
		t.Fatalf("err = %v", err)
	}
}

func TestSortScoreAfford(t *testing.T) {
	items := []Item{
		{ID: 1, Price: cents(12000), Stars: 4, Wanters: 2, SavesMoney: true, CreatedAt: 1}, // 4*2/120*1.5 = 0.1
		{ID: 2, Price: cents(2000), Stars: 3, Wanters: 1, CreatedAt: 2},                    // 0.15
		{ID: 3, Price: nil, Stars: 5, Wanters: 1, CreatedAt: 3},
		{ID: 4, Price: cents(50000), Stars: 5, Wanters: 1, CreatedAt: 4}, // 0.01
	}
	ids := func() []int64 {
		var out []int64
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	Sort(items, SortAdded)
	if got := ids(); !equal(got, []int64{4, 3, 2, 1}) {
		t.Errorf("added = %v", got)
	}
	Sort(items, SortPrice)
	if got := ids(); !equal(got, []int64{2, 1, 4, 3}) {
		t.Errorf("price = %v", got)
	}
	Sort(items, SortScore)
	if got := ids(); !equal(got, []int64{2, 1, 4, 3}) {
		t.Errorf("score = %v", got)
	}
	if s := ScoreText(items[1]); s != "4★ × 2 people ÷ $120 × 1.5" {
		t.Errorf("score text = %q", s)
	}

	// $150 now, $600 by month end: #2 ($20) and #1 ($120) now ($10 left now, $460 by month
	// end), #4 ($500) doesn't fit.
	a := Afford(items, 15000, 60000)
	if a[2] != AffordNow || a[1] != AffordNow || a[4] != "" || a[3] != "" {
		t.Errorf("afford = %v", a)
	}
	// $100 now, $700 by month end: #2 now, #1 by month end (then only $80 left now, $560 by
	// month end... and #1's claim leaves 560 ≥ 500 so #4 is month end too).
	a = Afford(items, 10000, 70000)
	if a[2] != AffordNow || a[1] != AffordMonthEnd || a[4] != AffordMonthEnd {
		t.Errorf("afford 2 = %v", a)
	}
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestShrink(t *testing.T) {
	out, mime, err := Shrink(fixture(t, "lamp.png"))
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("shrink: %v %s", err, mime)
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != 400 || img.Bounds().Dy() != 266 {
		t.Fatalf("decoded %v %v", img.Bounds(), err)
	}
	if _, _, err := Shrink([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)); err != ErrBadImage {
		t.Fatalf("svg accepted: %v", err)
	}
}

func TestAIFill(t *testing.T) {
	ts := httptest.NewServer(&fakeai.Server{})
	defer ts.Close()
	client := ai.New(ts.URL, "test-key", "light")
	u, _ := url.Parse("https://woodworks.example/bench")
	p := Parse(u, fixture(t, "textonly.html"))
	if err := AIFill(context.Background(), client, &p); err != nil {
		t.Fatal(err)
	}
	if p.Price == nil || *p.Price != 21900 || p.Blocked {
		t.Fatalf("ai price = %v blocked=%v", p.Price, p.Blocked)
	}

	// A price that isn't on the page is dropped.
	p = Preview{Title: "Bench", Text: "Garden bench, call for price"}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"price\":\"$19.99\",\"title\":null}"}}]}`))
	}))
	defer fake.Close()
	if err := AIFill(context.Background(), ai.New(fake.URL, "k", "m"), &p); err != nil || p.Price != nil {
		t.Fatalf("made-up price kept: %v %v", p.Price, err)
	}
}
