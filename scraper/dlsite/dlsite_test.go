package dlsite_test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"testing"

	"izumi/config"
	"izumi/scraper/dlsite"

	"github.com/PuerkitoBio/goquery"
)

var dlSiteScraper *dlsite.DlSite

func init() {
	// os.Setenv("https_proxy", "socks://127.0.0.1:7890")
	// os.Setenv("http_proxy", "socks://127.0.0.1:7890")
	// os.Setenv("all_proxy", "socks://127.0.0.1:7890")
	os.Setenv("https_proxy", "socks5://127.0.0.1:7890")
	os.Setenv("http_proxy", "socks5://127.0.0.1:7890")
	os.Setenv("all_proxy", "socks5://127.0.0.1:7890")
	scraper := dlsite.NewDlSite(map[string]string{
		"User-Agent":         config.DefaultUserAgent,
		"Accept-Language":    config.ZhLanguage,
		"Cookie":             dlsite.DefaultHeader_Cookie,
		"Sec-Ch-Ua":          dlsite.DefaultHeader_SecChUa,
		"Sec-Ch-Ua-Mobile":   dlsite.DefaultHeader_SecChUaMobile,
		"Sec-Ch-Ua-Platform": dlsite.DefaultHeader_SecChUaPlatform,
	}, "")
	dlSiteScraper = scraper.(*dlsite.DlSite)
}

func TestSearch(t *testing.T) {
	items, err := dlSiteScraper.SearchGame("Agent mirai", 1)
	if err != nil {
		panic(err)
	}
	for _, item := range items {
		fmt.Printf("%s ---- %s\n", item.Name, item.Cover)
	}
}

func TestGetItemCover(t *testing.T) {
	data, err := dlSiteScraper.DoReq(http.MethodGet, "https://www.dlsite.com/pro/work/=/product_id/VJ01001190.html?locale=zh_CN", nil, nil)
	if err != nil {
		panic(err)
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}
	cover, images, err := dlSiteScraper.GetItemCover(root)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s, %+v", cover, images)
}

func TestGetItemCharacter(t *testing.T) {
	data, err := dlSiteScraper.DoReq(http.MethodGet, "https://www.dlsite.com/pro/work/=/product_id/VJ01001190.html", nil, nil)
	if err != nil {
		panic(err)
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}
	characters, _, err := dlSiteScraper.GetItemCharacter(root)
	if err != nil {
		panic(err)
	}
	for _, c := range characters {
		fmt.Printf("%+v\n", c.CV)
	}
}

func TestGetItemStory(t *testing.T) {
	data, err := dlSiteScraper.DoReq(http.MethodGet, "https://www.dlsite.com/pro/work/=/product_id/VJ01001190.html", nil, nil)
	if err != nil {
		panic(err)
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}
	story, images, err := dlSiteScraper.GetItemStory(root)
	if err != nil {
		panic(err)
	}
	fmt.Println(story)
	fmt.Println(images)
}

func TestGetItemlinks(t *testing.T) {
	data, err := dlSiteScraper.DoReq(http.MethodGet, "https://www.dlsite.com/pro/work/=/product_id/VJ01001190.html", nil, nil)
	if err != nil {
		panic(err)
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		panic(err)
	}
	links, err := dlSiteScraper.GetItemlink(root, "VJ01001190")
	if err != nil {
		panic(err)
	}
	for _, l := range links {
		fmt.Printf("%+v", l)
	}
}

func TestGetItem(t *testing.T) {
	item, err := dlSiteScraper.GetGameItem("https://www.dlsite.com/pro/work/=/product_id/VJ01001190.html")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v", item.Tags)
}

func TestReq(t *testing.T) {
	data, err := dlSiteScraper.DoReq(http.MethodGet, "https://www.dlsite.com/pro/work/=/product_id/VJ01001190.html", nil, nil)
	if err != nil {
		panic(err)
	}
	f, err := os.Create("index.html")
	if err != nil {
		panic(err)
	}
	fmt.Fprint(f, string(data))
}
