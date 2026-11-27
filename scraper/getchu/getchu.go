package getchu

import (
	"bytes"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"izumi/internal/handler"
	"izumi/model"
	"izumi/scraper"
	"izumi/scraper/httpx"
	"izumi/utils"

	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

	"izumi/config"

	"github.com/PuerkitoBio/goquery"
	zaplog "github.com/dokidokikoi/go-common/log/zap"
	"github.com/dokidokikoi/go-common/tools"
	"go.uber.org/zap"
)

const Name = "getchu"

const (
	DefaultHeader_SecChUa         = `"Google Chrome";v="125", "Chromium";v="125", "Not.A/Brand";v="24"`
	DefaultHeader_SecChUaPlatform = `"macOS"`
	DefaultHeader_SecChUaMobile   = "?0"
	DefaultHeader_Referer         = "https://www.getchu.com/php/search.phtml?search_keyword=%C8%E0%BD%F7&list_count=30&sort=sales&sort2=down&search_title=&search_brand=&search_person=&search_jan=&search_isbn=&genre=pc_soft&start_date=&end_date=&age=&list_type=list&search=1&pageID=1"
	DefaultHeader_Cookie          = "_im_vid=01HYF9KCRA1MT8HSM4EWETGX8S; _gid=GA1.2.1781699859.1717215574; getchu_adalt_flag=getchu.com; ITEM_HISTORY=1282568%7C1273918; _ga_BSNR8334HV=GS1.1.1717222828.5.1.1717225315.53.0.0; _ga_JBMY6G3QFS=GS1.1.1717222828.5.1.1717225315.53.0.0; _ga=GA1.2.1343565952.1716352800; _gat=1"
)

var (
	GetChuDomain = "https://www.getchu.com/"
	GetChuSearch = "https://www.getchu.com/php/search.phtml?search_keyword=&list_count=30&sort=sales&sort2=down&search_title=%s&search_brand=&search_person=&search_jan=&search_isbn=&genre=pc_soft&start_date=&end_date=&age=&list_type=list&search=search&pageID=%d"
	GetChuImage  = "https://www.getchu.com/soft_sampleimage.phtml?id=%s"
)

type GetChu struct {
	sync.RWMutex
	name      string
	Domain    string
	SearchUri string
	Headers   map[string]string
	Proxy     string
	logger    *zap.Logger
	http      *httpx.Client
}

func NewGetChu(headers map[string]string, proxy string) scraper.IGameScraper {
	if headers == nil {
		headers = map[string]string{}
	}
	return &GetChu{
		name:      "getchu",
		Domain:    GetChuDomain,
		SearchUri: "",
		Headers:   headers,
		Proxy:     proxy,
		logger:    zaplog.L().Named(Name),
		http:      httpx.New(Name, proxy),
	}
}

func (gc *GetChu) GetName() string {
	return gc.name
}

func (gc *GetChu) SetHeader(header map[string]string) {
	gc.RLock()
	maps.Copy(gc.Headers, header)
	gc.RUnlock()
}

func (gc *GetChu) SetProxy(proxy string) {
	gc.RLock()
	gc.Proxy = proxy
	gc.RUnlock()
	gc.http.SetProxy(proxy)
}

func (gc *GetChu) SearchGame(keyword string, page int) ([]*scraper.SearchItem, error) {
	keyword, err := tools.Utf82Jp([]byte(strings.ReplaceAll(strings.TrimSpace(keyword), " ", ",")))
	if err != nil {
		return nil, err
	}
	keyword = tools.UrlEnc(keyword)
	url := fmt.Sprintf(GetChuSearch, keyword, page)

	data, err := gc.DoReq(http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	var urls []string
	var items []*scraper.SearchItem
	root.Find("div.search_container ul.display li").Each(func(i int, s *goquery.Selection) {
		s = s.Find("div.content_block")
		name, err := tools.Jp2Utf8([]byte(s.Find("#detail_block td").Eq(0).Find("a").Eq(0).Text()))
		if err != nil {
			gc.logger.Error("jp encode err", zap.Error(err))
		}
		url := s.Find("#package_block a img").AttrOr("data-original", "")
		if !strings.Contains(url, "news/now.jpg") {
			urls = append(urls, url)
		} else {
			url = ""
		}

		items = append(items, &scraper.SearchItem{
			Name:        name,
			URl:         gc.AbsUrl(s.Find("#package_block a").AttrOr("href", "")),
			Cover:       url,
			ScraperName: gc.name,
		})
	})

	m := utils.SaveBunchTmpFile(func(url string) ([]byte, error) {
		return gc.DoReq(http.MethodGet, url, nil, nil)
	}, urls)
	for _, item := range items {
		item.Cover = m[item.Cover]
	}

	return items, nil
}

func (gc *GetChu) DoReq(method, uri string, header map[string]string, body interface{}) ([]byte, error) {
	h := map[string]string{}
	gc.Lock()
	maps.Copy(h, gc.Headers)
	gc.Unlock()
	maps.Copy(h, header)

	query := tools.GenQueryParams(body)
	if query != "" {
		uri += "?" + query
	}

	rsp, err := gc.http.Do(method, uri, h, body)
	if err != nil {
		return nil, err
	}

	if rsp.StatusCode()/100 != 2 {
		return nil, errors.Errorf("status code: %d, body: %s", rsp.StatusCode(), rsp.String())
	}
	return rsp.Bytes(), nil
}

func (gc *GetChu) AbsUrl(uri string) string {
	if strings.Contains(uri, "news/now.jpg") {
		return ""
	}
	return tools.AbsUrl(gc.Domain, uri)
}

func (gc *GetChu) GetGameItem(uri string) (*scraper.GameItem, error) {
	data, err := gc.DoReq(http.MethodGet, uri, nil, nil)
	if err != nil {
		return nil, err
	}
	item := &scraper.GameItem{GameVo: handler.GameVo{Links: []model.Link{{Name: gc.name, Url: uri, Type: model.LinkTypeSource}}}, ScraperName: gc.name}
	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, errors.Wrapf(err, "url.Parse(%s)", uri)
	}
	kv := u.Query()
	id := kv.Get("id")
	if id == "" {
		return nil, errors.Errorf("id not found, uri: %s", uri)
	}
	item.RelIDs = append(item.RelIDs, fmt.Sprintf("%s:%s", Name, id))

	group := errgroup.Group{}
	group.Go(func() error {
		item.Cover, item.Images, err = gc.GetItemCover(root, id)
		if err != nil {
			gc.logger.Error("获取封面失败", zap.String("scraper", gc.name), zap.String("uri", uri), zap.Error(err))
		}
		item.Name, err = gc.GetItemName(root)
		if err != nil {
			gc.logger.Error("获取名称失败", zap.String("scraper", gc.name), zap.String("uri", uri), zap.Error(err))
		}
		return nil
	})

	root.Find("#soft_table tr:nth-child(2) table tr").Each(func(i int, s *goquery.Selection) {
		title, err := tools.Jp2Utf8([]byte(s.Find("td").First().Text()))
		if err != nil {
			gc.logger.Error("jp 解码错误", zap.Error(err))
			return
		}

		content := s.Find("td").Eq(1).Text()
		content, err = tools.Jp2Utf8([]byte(content))
		if err != nil {
			gc.logger.Error("jp 解码错误", zap.Error(err))
			return
		}

		nameSet := map[string]int{}

		title = tools.TrimBlankChar(title)
		if strings.Contains(title, "ブランド") {
			item.Brands = []*model.Brand{
				{Name: content},
			}
		} else if strings.Contains(title, "定価") {
			item.Price = content
		} else if strings.Contains(title, "発売日") {
			item.IssueDate = tools.Str2Time(content)
		} else if strings.Contains(title, "原画") {
			s.Find("td").Eq(1).Find("a").Each(func(i int, s *goquery.Selection) {
				name, err := tools.Jp2Utf8([]byte(s.Text()))
				if err != nil {
					gc.logger.Error("jp 解码错误", zap.Error(err))
					return
				}
				name = tools.TrimBlankChar(name)

				if idx, ok := nameSet[name]; ok {
					item.Staff[idx].Relation = append(item.Staff[idx].Relation, model.PRelationPainter)
				} else {
					item.Staff = append(item.Staff, handler.StaffVo{
						Name:     name,
						Relation: []model.PersonRelation{model.PRelationPainter},
					})
					nameSet[name] = len(item.Staff) - 1
				}
			})
		} else if strings.Contains(title, "シナリオ") {
			s.Find("td").Eq(1).Find("a").Each(func(i int, s *goquery.Selection) {
				name, err := tools.Jp2Utf8([]byte(s.Text()))
				if err != nil {
					gc.logger.Error("jp 解码错误", zap.Error(err))
					return
				}
				name = tools.TrimBlankChar(name)

				if idx, ok := nameSet[name]; ok {
					item.Staff[idx].Relation = append(item.Staff[idx].Relation, model.PRelationWriter)
				} else {
					item.Staff = append(item.Staff, handler.StaffVo{
						Name:     name,
						Relation: []model.PersonRelation{model.PRelationWriter},
					})
					nameSet[name] = len(item.Staff) - 1
				}
			})
		}
	})

	group.Go(func() error {
		var staff []handler.StaffVo
		item.Characters, staff, err = gc.GetItemCharacter(root)
		if err != nil {
			gc.logger.Error("获取角色失败", zap.String("scraper", gc.name), zap.String("uri", uri), zap.Error(err))
		}
		item.Staff = append(item.Staff, staff...)
		item.Story, err = gc.GetItemStory(root)
		if err != nil {
			gc.logger.Error("获取故事失败", zap.String("scraper", gc.name), zap.String("uri", uri), zap.Error(err))
		}
		return nil
	})

	group.Wait()

	return item, nil
}

func (gc *GetChu) GetItemName(node *goquery.Document) (string, error) {
	return tools.Jp2Utf8([]byte(tools.TrimBlankChar(node.Find("#soft-title").Text())))
}

func (gc *GetChu) GetItemCover(node *goquery.Document, id string) (cover string, images []string, err error) {
	cover = gc.AbsUrl(node.Find("#soft_table tr").First().Find("a").First().Find("img").AttrOr("src", ""))

	urls := []string{}
	if cover != "" {
		urls = append(urls, cover)
	}
	defer func() {
		res := utils.SaveBunchTmpFile(func(url string) ([]byte, error) {
			return gc.DoReq(http.MethodGet, url, nil, nil)
		}, urls)

		for _, url := range urls {
			path, ok := res[url]
			if ok {
				if url == cover {
					cover = strings.TrimPrefix(path, config.TmpDir)
				} else {
					images = append(images, strings.TrimPrefix(path, config.TmpDir))
				}
			}
		}
	}()

	node.Find(".tabletitle").Each(func(i int, selection *goquery.Selection) {
		title, err := tools.Jp2Utf8([]byte(selection.Text()))
		if err != nil {
			return
		}
		if strings.Contains(title, "サンプル画像") {
			selection.Next().Find(`div.item-Samplecard`).Each(func(i int, s *goquery.Selection) {
				image := s.Find("a").AttrOr("href", "")
				urls = append(urls, gc.AbsUrl(image))
			})
		}
	})

	return
}

func (gc *GetChu) GetItemStory(node *goquery.Document) (string, error) {
	var story string
	node.Find("div.tabletitle").Each(func(i int, selection *goquery.Selection) {
		title, err := tools.Jp2Utf8([]byte(selection.Text()))
		if err != nil {
			return
		}
		if strings.Contains(title, "ストーリー") {
			story, err = tools.Jp2Utf8([]byte(selection.Next().Text()))
			if err != nil {
				return
			}
			return
		}
	})
	return tools.TrimBlankChar(story), nil
}

func (gc *GetChu) GetItemCharacter(node *goquery.Document) ([]handler.CharacterVo, []handler.StaffVo, error) {
	var characters []handler.CharacterVo
	var staff []handler.StaffVo
	node.Find(".tabletitle").Each(func(i int, selection *goquery.Selection) {
		title, err := tools.Jp2Utf8([]byte(selection.Text()))
		if err != nil {
			return
		}
		urls := []string{}
		if strings.Contains(title, "キャラクター") {
			trs := selection.Next().Find(`tr`)
			trs.Each(func(i int, selection *goquery.Selection) {
				if selection.Find("hr").Length() > 0 {
					return
				}
				avatar, _ := selection.Find("td:nth-child(1) img").Attr("src")
				nameText, err := tools.Jp2Utf8([]byte(selection.Find("dt h4.chara-name").Text()))
				if err != nil {
					gc.logger.With(zap.Error(err)).Error("tools.Jp2Utf8")
				}
				alias := ""
				aliasIdxStart := strings.Index(nameText, "（")
				aliasIdxEnd := strings.Index(nameText, "）")
				if aliasIdxStart > 0 && aliasIdxEnd > 0 {
					alias = tools.TrimBlankChar(nameText[aliasIdxStart+3 : aliasIdxEnd])
				}
				CVIdx := strings.Index(nameText, "CV")
				var s handler.StaffVo
				if CVIdx > 0 && len(nameText) > CVIdx+5 {
					s = handler.StaffVo{
						Name: tools.TrimBlankChar(nameText[CVIdx+5:]),
						Relation: []model.PersonRelation{
							model.PRelationCV,
						},
					}
					staff = append(staff, s)
				}
				name := nameText
				if aliasIdxStart != -1 && CVIdx != -1 {
					name = tools.TrimBlankChar(nameText[:min(aliasIdxStart, CVIdx)])
				} else if aliasIdxStart != -1 {
					name = tools.TrimBlankChar(nameText[:aliasIdxStart])
				} else if CVIdx != -1 {
					name = tools.TrimBlankChar(nameText[:CVIdx])
				}
				text, _ := selection.Find("td:nth-child(2) dd").Html()
				introduction, err := tools.Jp2Utf8([]byte(text))
				if err != nil {
					gc.logger.With(zap.Error(err)).Error("tools.Jp2Utf8")
				}
				introduction = tools.TrimBlankChar(introduction)
				image, _ := selection.Find("td a.highslide").Attr("href")
				characters = append(characters, handler.CharacterVo{
					Name:    name,
					Alias:   []string{alias},
					Summary: introduction,
					Cover:   gc.AbsUrl(avatar),
					CV:      s,
					Images:  []string{gc.AbsUrl(image)},
				})

				urls = append(urls, gc.AbsUrl(image), gc.AbsUrl(avatar))
			})

			res := utils.SaveBunchTmpFile(func(url string) ([]byte, error) {
				return gc.DoReq(http.MethodGet, url, nil, nil)
			}, urls)
			for i := range characters {
				if path, ok := res[characters[i].Cover]; ok {
					characters[i].Cover = strings.TrimPrefix(path, config.TmpDir)
				}
				var images []string
				for _, image := range characters[i].Images {
					if path, ok := res[image]; ok {
						images = append(images, strings.TrimPrefix(path, config.TmpDir))
					}
				}
				characters[i].Images = images
			}
			return
		}
	})

	return characters, staff, nil
}
