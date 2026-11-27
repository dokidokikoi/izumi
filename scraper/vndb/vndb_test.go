package vndb_test

import (
	"fmt"
	"os"
	"testing"

	"izumi/scraper/vndb"
)

var vndbScraper *vndb.VNDB

func init() {
	os.Setenv("https_proxy", "socks://127.0.0.1:7890")
	os.Setenv("http_proxy", "socks://127.0.0.1:7890")
	os.Setenv("all_proxy", "socks://127.0.0.1:7890")
	vndbScraper = vndb.NewVNDB(nil, "").(*vndb.VNDB)
}

func Test_GetItem(t *testing.T) {
	item, err := vndbScraper.GetGameItem("v7636")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(item)
}

func Test_FetchCharacters(t *testing.T) {
	cs, err := vndbScraper.FetchCharacters("v7636")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(len(cs))
}
