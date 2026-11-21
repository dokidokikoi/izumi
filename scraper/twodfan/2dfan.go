package twodfan

import (
	"bytes"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"izumi/config"
	"izumi/internal/handler"
	"izumi/model"
	"izumi/scraper"
	"izumi/utils"

	"github.com/PuerkitoBio/goquery"
	zaplog "github.com/dokidokikoi/go-common/log/zap"
	"github.com/dokidokikoi/go-common/tools"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const Name = "2dfan"

const (
	DefaultHeader_Cookie = ""
)

var (
	twoDFanDomain    = "https://2dfan.com/"
	twoDFanSearchUri = "https://2dfan.com/subjects/search%s?keyword=%s"
)

type TwoDFan struct {
	sync.RWMutex
	name      string
	Domain    string
	SearchUri string
	Headers   map[string]string
	Proxy     string
	logger    *zap.Logger
}

func NewTwoDFan(header map[string]string, proxy string) scraper.IGameScraper {
	return &TwoDFan{
		name:      Name,
		Domain:    twoDFanDomain,
		SearchUri: twoDFanSearchUri,
		Headers:   header,
		Proxy:     proxy,
		logger:    zaplog.L().Named(Name),
	}
}

func (tdf *TwoDFan) GetName() string {
	return tdf.name
}

func (tdf *TwoDFan) SetHeader(header map[string]string) {
	tdf.Lock()
	maps.Copy(tdf.Headers, header)
	tdf.Unlock()
}

func (tdf *TwoDFan) SetProxy(proxy string) {
	tdf.Lock()
	tdf.Proxy = proxy
	tdf.Unlock()
}

func (tdf *TwoDFan) SearchGame(keyword string, page int) ([]*scraper.SearchItem, error) {
	u := ""
	if page > 1 {
		u = fmt.Sprintf(tdf.SearchUri, fmt.Sprintf("/page/%d", page), strings.ReplaceAll(strings.TrimSpace(keyword), " ", "+"))
	} else {
		u = fmt.Sprintf(tdf.SearchUri, "", keyword)
	}

	uri, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	uri.RawQuery = uri.Query().Encode()
	data, err := tdf.DoReq(http.MethodGet, uri.String(), nil, nil)
	if err != nil {
		return nil, err
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	var images []string
	var items []*scraper.SearchItem
	root.Find("#subject-list div.subject-card").Each(func(i int, s *goquery.Selection) {
		url := s.Find("div.subject-card-cover img").AttrOr("src", "")
		images = append(images, url)
		items = append(items, &scraper.SearchItem{
			Name:        s.Find("h3.subject-card-title a").Text(),
			Key:         "",
			URl:         tdf.AbsUrl(s.Find("h3.subject-card-title a").AttrOr("href", "")),
			Summary:     "",
			Cover:       url,
			ScraperName: tdf.name,
		})
	})

	m := utils.SaveBunchTmpFile(func(url string) ([]byte, error) {
		return tdf.DoReq(http.MethodGet, url, map[string]string{"referer": tdf.Domain}, nil)
	}, images)
	for _, item := range items {
		item.Cover = m[item.Cover]
	}
	return items, nil
}

func (tdf *TwoDFan) DoReq(method, uri string, header map[string]string, body any) ([]byte, error) {
	h := map[string]string{}
	tdf.RLock()
	maps.Copy(h, tdf.Headers)
	tdf.RUnlock()
	maps.Copy(h, header)

	rsp, err := tools.ReqWithProxy(method, uri, body, tdf.Proxy, tools.SetHeadersWithOption(h))
	if err != nil {
		return nil, err
	}
	return rsp.Bytes(), nil
}

func (tdf *TwoDFan) AbsUrl(uri string) string {
	return tools.AbsUrl(tdf.Domain, uri)
}

func (tdf *TwoDFan) GetGameItem(uri string) (*scraper.GameItem, error) {
	data, err := tdf.DoReq(http.MethodGet, uri, nil, nil)
	if err != nil {
		return nil, err
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	item := &scraper.GameItem{GameVo: handler.GameVo{Links: []model.Link{{Name: tdf.name, Url: uri, Type: model.LinkTypeSource}}}, ScraperName: tdf.name}
	u, err := url.Parse(uri)
	if err == nil {
		arr := strings.Split(u.Path, "/")
		id := arr[len(arr)-1]
		if id != "" {
			item.RelIDs = append(item.RelIDs, fmt.Sprintf("%s:%s", Name, id))
		}
	}
	// 获取名称
	item.Name, item.Alias, err = tdf.GetItemName(root)
	if err != nil {
		tdf.logger.Error("获取名称失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	// 获取
	item.Category, err = tdf.GetItemCategory(root)
	if err != nil {
		tdf.logger.Error("获取分类失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.Cover, item.Images, err = tdf.GetItemCover(root)
	if err != nil {
		tdf.logger.Error("获取封面失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.IssueDate, err = tdf.GetItemIssueDate(root)
	if err != nil {
		tdf.logger.Error("获取发布日期失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.Characters, err = tdf.GetItemCharacters(root)
	if err != nil {
		tdf.logger.Error("获取角色失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.Brands, err = tdf.GetItemBrands(root)
	if err != nil {
		tdf.logger.Error("获取开发厂商失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.Tags, err = tdf.GetItemTags(root)
	if err != nil {
		tdf.logger.Error("获取tag失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.OtherInfo, err = tdf.GetItemOtherInfo(root)
	if err != nil {
		tdf.logger.Error("获取其它信息失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	links, err := tdf.GetItemLinks(root)
	if err != nil {
		tdf.logger.Error("获取链接失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.Links = append(item.Links, links...)
	var images []string
	item.Story, images, err = tdf.GetItemStory(root)
	if err != nil {
		tdf.logger.Error("获取故事失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}
	item.Images = append(item.Images, images...)
	item.Staff, err = tdf.GetItemStaff(root)
	if err != nil {
		tdf.logger.Error("获取staff失败", zap.String("scraper", tdf.name), zap.String("uri", uri), zap.Error(err))
	}

	return item, nil
}

func (tdf *TwoDFan) GetItemName(node *goquery.Document) (string, []string, error) {
	var alias []string
	node.Find("#content div.control-group p.tags").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Text(), "又名") {
			alias = append(alias, strings.Split(s.Find("span").Text(), "/")...)
		}
	})
	alias = strings.Split(node.Find("p.subject-aka").First().Text(), " / ")
	return node.Find("h1.subject-title").First().Text(), alias, nil
}

func (tdf *TwoDFan) GetItemCover(node *goquery.Document) (string, []string, error) {
	data := node.Find("div.subject-info-main div.subject-cover").First().AttrOr("data-props", "")
	image := gjson.Get(data, "src").String()
	imageMap := utils.SaveBunchTmpFile(func(uri string) ([]byte, error) {
		return tdf.DoReq(http.MethodGet, uri, nil, nil)
	}, []string{image})
	return imageMap[image], nil, nil
}

func (tdf *TwoDFan) GetItemCategory(node *goquery.Document) (*model.Category, error) {
	return nil, nil
}

func (tdf *TwoDFan) GetItemBrands(node *goquery.Document) ([]*model.Brand, error) {
	brands := []*model.Brand{}
	node.Find("div.subject-meta div.meta-row").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Find("dt").Text(), "品牌") {
			s.Find("a").Each(func(i int, s *goquery.Selection) {
				brands = append(brands, &model.Brand{
					Name: s.Text(),
				})
			})
		}
	})
	return brands, nil
}

func (tdf *TwoDFan) GetItemPrice(node *goquery.Document) (string, error) {
	return "", nil
}

func (tdf *TwoDFan) GetItemStory(node *goquery.Document) (string, []string, error) {
	url := node.Find("main.nexus-main a.view-more-link").First().AttrOr("href", "")
	if url == "" {
		return "", nil, nil
	}

	data, err := tdf.DoReq(http.MethodGet, tdf.AbsUrl(url), nil, nil)
	if err != nil {
		return "", nil, err
	}

	root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
	if err != nil {
		return "", nil, err
	}

	maxPage := 0
	href := root.Find("div.topic-content-pagination a.nexus-pagination-last").First().AttrOr("href", "")
	hrefArr := strings.Split(href, "?")
	if len(hrefArr) > 1 {
		querys := strings.Split(hrefArr[1], "&")
		for _, query := range querys {
			if strings.HasPrefix(query, "page") {
				maxPage, _ = strconv.Atoi(strings.TrimPrefix(query, "page="))
			}
		}
	}
	maxPage = max(1, maxPage)

	story := strings.Builder{}
	var images []string
	f := func(root *goquery.Document) {
		root.Find("#topic-content-body img").Each(func(i int, s *goquery.Selection) {
			imgUrl := s.AttrOr("src", "")
			if imgUrl == "" {
				return
			}

			images = append(images, imgUrl)
		})

		html, _ := root.Find("#topic-content-body").Html()
		story.WriteString(html)
		story.WriteByte('\n')
	}

	g := errgroup.Group{}
	g.SetLimit(1)
	for i := 1; i <= maxPage; i++ {
		g.Go(func() error {
			data, err := tdf.DoReq(http.MethodGet, tdf.AbsUrl(url+fmt.Sprintf("?page=%d", i)), nil, nil)
			if err != nil {
				return err
			}

			root, err := goquery.NewDocumentFromReader(bytes.NewBuffer(data))
			if err != nil {
				return err
			}

			f(root)
			return nil
		})
	}
	err = g.Wait()
	if err != nil {
		return "", nil, err
	}

	res := utils.SaveBunchTmpFile(func(url string) ([]byte, error) {
		return tdf.DoReq(http.MethodGet, url, nil, nil)
	}, images)
	images = images[:0]
	s := story.String()
	for k, v := range res {
		images = append(images, v)
		s = strings.ReplaceAll(s, k, filepath.Join("/api/file", strings.TrimPrefix(v, config.TmpDir)))
	}
	s = tools.TrimBlankChar(s)

	return s, images, nil
}

func (tdf *TwoDFan) GetItemSize(node *goquery.Document) (string, error) {
	return "", nil
}

func (tdf *TwoDFan) GetItemLinks(node *goquery.Document) ([]model.Link, error) {
	var links []model.Link
	node.Find("aside.nexus-right-sidebar section").Each(func(i int, s *goquery.Selection) {
		title := s.Find("h3.sidebar-section-title").Text()
		ty := model.LinkTypeOther
		if strings.Contains(title, "攻略") {
			ty = model.LinkTypeWorkThrough
		}
		s.Find("ul.related-list li").Each(func(i int, s *goquery.Selection) {
			url := s.Find("a").AttrOr("href", "")
			if url == "" {
				return
			}
			name := s.Find("a span.related-link__key").Text()
			links = append(links, model.Link{
				Name: name,
				Url:  tdf.AbsUrl(url),
				Type: ty,
			})
		})
	})

	return links, nil
}

func (tdf *TwoDFan) GetItemCharacters(node *goquery.Document) ([]handler.CharacterVo, error) {
	return nil, nil
}

func (tdf *TwoDFan) GetItemIssueDate(node *goquery.Document) (time.Time, error) {
	var t time.Time
	node.Find("div.subject-meta div.meta-row").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Find("dt").Text(), "发售日期") {
			t = tools.Str2Time(utils.DateExtarct(s.Text()))
		}
	})
	return t, nil
}

func (tdf *TwoDFan) GetItemTags(node *goquery.Document) ([]*model.Tag, error) {
	var tags []*model.Tag
	node.Find("div.subject-meta div.subject-tags-inline a").Each(func(i int, s *goquery.Selection) {
		tags = append(tags, &model.Tag{
			Name: s.Text(),
		})
	})

	return tags, nil
}

func (tdf *TwoDFan) GetItemStaff(node *goquery.Document) ([]handler.StaffVo, error) {
	var staff []handler.StaffVo
	staffMap := map[string]handler.StaffVo{}
	node.Find("div.subject-meta div.meta-row").Each(func(i int, s *goquery.Selection) {
		if strings.Contains(s.Find("dt").Text(), "原画") {
			s.Find("a").Each(func(i int, s *goquery.Selection) {
				sta, ok := staffMap[s.Text()]
				if ok {
					sta.Relation = append(sta.Relation, model.PRelationPainter)
				} else {
					sta = handler.StaffVo{
						Name:     s.Text(),
						Relation: []model.PersonRelation{model.PRelationPainter},
					}
				}
				staffMap[s.Text()] = sta
			})
		} else if strings.Contains(s.Find("dt").Text(), "剧本") {
			s.Find("a").Each(func(i int, s *goquery.Selection) {
				sta, ok := staffMap[s.Text()]
				if ok {
					sta.Relation = append(sta.Relation, model.PRelationPainter)
				} else {
					sta = handler.StaffVo{
						Name:     s.Text(),
						Relation: []model.PersonRelation{model.PRelationWriter},
					}
				}
				staffMap[s.Text()] = sta
			})
		} else if strings.Contains(s.Find("dt").Text(), "声优") {
			fmt.Println("cv")
			s.Find("a").Each(func(i int, s *goquery.Selection) {
				sta, ok := staffMap[s.Text()]
				if ok {
					sta.Relation = append(sta.Relation, model.PRelationCV)
				} else {
					sta = handler.StaffVo{
						Name:     s.Text(),
						Relation: []model.PersonRelation{model.PRelationCV},
					}
				}
				staffMap[s.Text()] = sta
			})
		} else if strings.Contains(s.Find("dt").Text(), "音乐") {
			s.Find("a").Each(func(i int, s *goquery.Selection) {
				sta, ok := staffMap[s.Text()]
				if ok {
					sta.Relation = append(sta.Relation, model.PRelationMusic)
				} else {
					sta = handler.StaffVo{
						Name:     s.Text(),
						Relation: []model.PersonRelation{model.PRelationMusic},
					}
				}
				staffMap[s.Text()] = sta
			})
		}
	})

	for _, v := range staffMap {
		staff = append(staff, v)
	}

	return staff, nil
}

func (tdf *TwoDFan) GetItemOtherInfo(node *goquery.Document) (string, error) {
	return "", nil
}
