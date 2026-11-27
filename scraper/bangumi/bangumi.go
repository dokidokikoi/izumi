package bangumi

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"izumi/config"
	"izumi/internal/handler"
	"izumi/model"
	"izumi/scraper"
	"izumi/scraper/httpx"
	"izumi/utils"

	zaplog "github.com/dokidokikoi/go-common/log/zap"
	"github.com/dokidokikoi/go-common/tools"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const Name = "bangumi"

var DefaultHeader_UserAgent = "dokidokikoi/izumi (https://github.com/dokidokikoi/izumi)"

var (
	BangumiDomain        = "https://api.bgm.tv"
	BangumiSearchUri     = BangumiDomain + "/v0/search/subjects"
	BangumiSubjectUri    = BangumiDomain + "/v0/subjects/%s"
	BangumiCharactersUri = BangumiDomain + "/v0/subjects/%s/characters"
	BangumiPersonsUri    = BangumiDomain + "/v0/subjects/%s/persons"

	BangumiCharactersInfoUri = BangumiDomain + "/v0/characters/%s"
	BangumiPersonsInfoUri    = BangumiDomain + "/v0/persons/%s"

	defaultPageSize = 20
)

const (
	SubjectTypeBook = iota + 1
	SubjectTypeAnime
	SubjectTypeMusic
	SubjectTypeGame
	SubjectTypeReal = 6
)

type Bangumi struct {
	sync.RWMutex
	name      string
	Domain    string
	SearchUri string
	Headers   map[string]string
	Proxy     string
	logger    *zap.Logger
	http      *httpx.Client
}

func NewBangumi(header map[string]string, proxy string) scraper.IGameScraper {
	if header == nil {
		header = map[string]string{}
	}
	return &Bangumi{
		name:      Name,
		Domain:    BangumiDomain,
		SearchUri: BangumiSearchUri,
		Headers:   header,
		Proxy:     proxy,
		logger:    zaplog.L().Named(Name),
		http:      httpx.New(Name, proxy),
	}
}

func (b *Bangumi) GetName() string {
	return b.name
}

func (b *Bangumi) SetHeader(header map[string]string) {
	b.Lock()
	maps.Copy(b.Headers, header)
	b.Unlock()
}

func (b *Bangumi) SetProxy(proxy string) {
	b.Lock()
	b.Proxy = proxy
	b.Unlock()
	b.http.SetProxy(proxy)
}

func (b *Bangumi) AbsUrl(uri string) string {
	return tools.AbsUrl(b.Domain, uri)
}

func (b *Bangumi) SearchGame(keyword string, page int) ([]*scraper.SearchItem, error) {
	param := SearchParam{
		Filter: SearchParamFilter{
			Nsfw: true,
			Type: []int{SubjectTypeGame},
		},
		Keyword: strings.ReplaceAll(strings.TrimSpace(keyword), " ", "+"),
		Limit:   defaultPageSize,
		Offset:  (page - 1) * defaultPageSize,
	}
	data, err := b.DoReq(http.MethodPost, b.SearchUri, nil, param)
	if err != nil {
		zaplog.L().Error("bangumi search error", zap.Error(err))
		return nil, err
	}
	var items []*scraper.SearchItem
	for _, i := range gjson.GetBytes(data, "data").Array() {
		items = append(items, &scraper.SearchItem{
			Name:    i.Get("name").String(),
			Key:     strconv.Itoa(int(i.Get("id").Int())),
			URl:     fmt.Sprintf(BangumiSubjectUri, strconv.Itoa(int(i.Get("id").Int()))),
			Summary: i.Get("summary").String(),
			Cover: func() string {
				cover := i.Get("images.large").String()
				if cover == "" {
					cover = i.Get("images.medium").String()
				}
				if cover == "" {
					cover = i.Get("images.common").String()
				}
				return cover
			}(),
			ScraperName: b.name,
		})
	}
	return items, nil
}

func (b *Bangumi) GetGameItem(uri string) (*scraper.GameItem, error) {
	data, err := b.DoReq(http.MethodGet, uri, nil, nil)
	if err != nil {
		return nil, err
	}

	id := ""
	u, _ := url.Parse(uri)
	if u != nil {
		arr := strings.Split(u.Path, "/")
		id = arr[len(arr)-1]
	}
	item := &scraper.GameItem{GameVo: handler.GameVo{Links: []model.Link{{Name: b.name, Url: "https://bangumi.tv/subject/" + id, Type: model.LinkTypeSource}}}, ScraperName: b.name}
	// 获取名称
	item.Name = gjson.GetBytes(data, "name").String()
	nameCN := gjson.GetBytes(data, "name_cn").String()
	if nameCN != "" {
		item.Alias = []string{nameCN}
	}

	cover := pickImageURL(gjson.ParseBytes(data))
	item.Story = gjson.GetBytes(data, "summary").String()
	for _, tag := range gjson.GetBytes(data, "tags").Array() {
		item.Tags = append(item.Tags, &model.Tag{
			Name: tag.Get("name").String(),
		})
	}
	for _, info := range gjson.GetBytes(data, "infobox").Array() {
		switch info.Get("key").String() {
		case "别名":
			for _, alias := range info.Get("value").Array() {
				item.Alias = append(item.Alias, alias.Get("v").String())
			}
		case "游戏类型":
			item.Category = &model.Category{
				Name: info.Get("value").String(),
			}
		case "发行日期":
			item.IssueDate = tools.Str2Time(info.Get("value").String())
		case "售价":
			item.Price = info.Get("value").String()
		case "website":
			item.Links = append(item.Links, model.Link{
				Name: "官网",
				Url:  info.Get("value").String(),
				Type: model.LinkTypeOfficial,
			})
		}
	}
	if id != "" {
		item.RelIDs = append(item.RelIDs, fmt.Sprintf("%s:%s", Name, id))
	}

	// 封面下载、角色、职员三个阶段互不依赖，并行执行。
	var (
		coverPath  string
		characters []handler.CharacterVo
		charStaff  []handler.StaffVo
		staff      []handler.StaffVo
	)
	group := errgroup.Group{}
	group.Go(func() error {
		res := utils.SaveBunchTmpFile(func(u string) ([]byte, error) {
			return b.DoReq(http.MethodGet, u, nil, nil)
		}, []string{cover})
		coverPath = strings.TrimPrefix(res[cover], config.TmpDir)
		return nil
	})
	group.Go(func() error {
		var e error
		characters, charStaff, e = b.GetItemCharacters(id)
		if e != nil {
			b.logger.Error("get characters error", zap.String("scraper", b.name), zap.Error(e))
		}
		return nil
	})
	group.Go(func() error {
		var e error
		staff, e = b.GetItemStaff(id)
		if e != nil {
			b.logger.Error("get staff error", zap.String("scraper", b.name), zap.Error(e))
		}
		return nil
	})
	group.Wait()

	item.Cover = coverPath
	item.Characters = characters
	item.Staff = append(charStaff, staff...)
	return item, nil
}

// pickImageURL 按 large -> medium -> common 的顺序取图片地址。
func pickImageURL(v gjson.Result) string {
	cover := v.Get("images.large").String()
	if cover == "" {
		cover = v.Get("images.medium").String()
	}
	if cover == "" {
		cover = v.Get("images.common").String()
	}
	return cover
}

func (b *Bangumi) GetItemCharacters(SubjetID string) ([]handler.CharacterVo, []handler.StaffVo, error) {
	data, err := b.DoReq(http.MethodGet, fmt.Sprintf(BangumiCharactersUri, SubjetID), nil, nil)
	if err != nil {
		return nil, nil, err
	}

	type charEntry struct {
		id                            int64
		name, relation                string
		coverURL, coverPath           string
		actorID, actorName            string
		actorCoverURL, actorCoverPath string
		info, actorInfo               gjson.Result
		infoDone, actorDone           bool
	}

	entries := []charEntry{}
	coverURLs := []string{}
	for _, c := range gjson.ParseBytes(data).Array() {
		e := charEntry{
			id:       c.Get("id").Int(),
			name:     c.Get("name").String(),
			relation: c.Get("relation").String(),
			coverURL: pickImageURL(c),
		}
		if e.coverURL != "" {
			coverURLs = append(coverURLs, e.coverURL)
		}
		if arr := c.Get("actors").Array(); len(arr) > 0 {
			e.actorID = arr[0].Get("id").String()
			e.actorName = arr[0].Get("name").String()
			e.actorCoverURL = pickImageURL(arr[0])
			if e.actorCoverURL != "" {
				coverURLs = append(coverURLs, e.actorCoverURL)
			}
		}
		entries = append(entries, e)
	}

	// 角色封面与 CV 封面一次性批量下载。
	saved := utils.SaveBunchTmpFiles(func(u string) ([]byte, error) {
		return b.DoReq(http.MethodGet, u, nil, nil)
	}, coverURLs)
	paths := make(map[string]string, len(saved))
	for _, f := range saved {
		paths[f.URL] = f.Path
	}
	for i := range entries {
		entries[i].coverPath = paths[entries[i].coverURL]
		entries[i].actorCoverPath = paths[entries[i].actorCoverURL]
	}

	// 角色详情与 CV 详情有界并发，结果按下标写回，保持列表顺序。
	group := errgroup.Group{}
	group.SetLimit(max(1, b.http.MaxConcurrency()))
	for i := range entries {
		group.Go(func() error {
			e := &entries[i]
			infoURL := fmt.Sprintf(BangumiCharactersInfoUri, strconv.Itoa(int(e.id)))
			infoData, err := b.DoReq(http.MethodGet, infoURL, nil, nil)
			if err != nil {
				b.logger.Error("request error", zap.String("url", infoURL), zap.Error(err))
				return nil
			}
			e.info = gjson.ParseBytes(infoData)
			e.infoDone = true

			if e.actorID == "" {
				return nil
			}
			actorURL := fmt.Sprintf(BangumiPersonsInfoUri, e.actorID)
			actorData, err := b.DoReq(http.MethodGet, actorURL, nil, nil)
			if err != nil {
				b.logger.Error("request error", zap.String("url", actorURL), zap.Error(err))
				return nil
			}
			e.actorInfo = gjson.ParseBytes(actorData)
			e.actorDone = true
			return nil
		})
	}
	group.Wait()

	characters := make([]handler.CharacterVo, 0, len(entries))
	staff := make([]handler.StaffVo, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if !e.infoDone {
			continue
		}
		cc := e.info

		var alias []string
		for _, a := range cc.Get("infobox").Array() {
			if a.Get("key").String() == "别名" {
				for _, ali := range a.Get("value").Array() {
					alias = append(alias, ali.Get("v").String())
				}
			}
		}
		character := handler.CharacterVo{
			RelIDs:  []string{fmt.Sprintf("%s:%d", Name, e.id)},
			Name:    e.name,
			Rlation: model.CharacterRelation(e.relation),
			Cover:   strings.TrimPrefix(e.coverPath, config.TmpDir),
			Gender:  Gender(cc.Get("gender").String()),
			Summary: cc.Get("summary").String(),
			Alias:   alias,
			PersonalInfo: model.PersonalInfo{
				Birthday:  [2]int{int(cc.Get("birth_mon").Int()), int(cc.Get("birth_day").Int())},
				BrithYear: int(cc.Get("birth_year").Int()),
				BloodType: BloodType(int(cc.Get("blood_type").Int())),
			},
		}
		if e.actorID != "" {
			// 保持旧行为：CV 详情失败时跳过整个角色。
			if !e.actorDone {
				continue
			}
			character.CV = handler.StaffVo{
				RelIDs:   []string{fmt.Sprintf("%s:%s", Name, e.actorID)},
				Name:     e.actorName,
				Cover:    strings.TrimPrefix(e.actorCoverPath, config.TmpDir),
				Summary:  e.actorInfo.Get("short_summary").String(),
				Gender:   Gender(e.actorInfo.Get("gender").String()),
				Relation: []model.PersonRelation{model.PRelationCV},
			}
			staff = append(staff, character.CV)
		}
		characters = append(characters, character)
	}
	return characters, staff, nil
}

func (b *Bangumi) GetItemStaff(SubjectID string) ([]handler.StaffVo, error) {
	data, err := b.DoReq(http.MethodGet, fmt.Sprintf(BangumiPersonsUri, SubjectID), nil, nil)
	if err != nil {
		return nil, err
	}

	type staffEntry struct {
		id        int64
		name      string
		coverURL  string
		coverPath string
		relations []model.PersonRelation
		info      gjson.Result
		done      bool
	}

	order := []int64{}
	byID := map[int64]*staffEntry{}
	for _, s := range gjson.ParseBytes(data).Array() {
		id := s.Get("id").Int()
		e, ok := byID[id]
		if !ok {
			e = &staffEntry{
				id:       id,
				name:     s.Get("name").String(),
				coverURL: pickImageURL(s),
			}
			byID[id] = e
			order = append(order, id)
		}
		e.relations = append(e.relations, PersonRelation(s.Get("relation").String()))
	}

	coverURLs := make([]string, 0, len(order))
	for _, id := range order {
		if byID[id].coverURL != "" {
			coverURLs = append(coverURLs, byID[id].coverURL)
		}
	}
	saved := utils.SaveBunchTmpFiles(func(u string) ([]byte, error) {
		return b.DoReq(http.MethodGet, u, nil, nil)
	}, coverURLs)
	paths := make(map[string]string, len(saved))
	for _, f := range saved {
		paths[f.URL] = f.Path
	}
	for _, id := range order {
		byID[id].coverPath = paths[byID[id].coverURL]
	}

	group := errgroup.Group{}
	group.SetLimit(max(1, b.http.MaxConcurrency()))
	for _, id := range order {
		group.Go(func() error {
			e := byID[id]
			infoURL := fmt.Sprintf(BangumiPersonsInfoUri, strconv.Itoa(int(id)))
			infoData, err := b.DoReq(http.MethodGet, infoURL, nil, nil)
			if err != nil {
				b.logger.Error("request error", zap.String("url", infoURL), zap.Error(err))
				return nil
			}
			e.info = gjson.ParseBytes(infoData)
			e.done = true
			return nil
		})
	}
	group.Wait()

	staff := make([]handler.StaffVo, 0, len(order))
	for _, id := range order {
		e := byID[id]
		if !e.done {
			continue
		}
		var alias []string
		for _, a := range e.info.Get("infobox").Array() {
			if a.Get("key").String() == "别名" {
				for _, ali := range a.Get("value").Array() {
					alias = append(alias, ali.Get("v").String())
				}
			}
		}
		staff = append(staff, handler.StaffVo{
			RelIDs:   []string{fmt.Sprintf("%s:%d", Name, id)},
			Name:     e.name,
			Cover:    strings.TrimPrefix(e.coverPath, config.TmpDir),
			Relation: e.relations,
			Gender:   Gender(e.info.Get("gender").String()),
			Summary:  e.info.Get("short_summary").String(),
			Alias:    alias,
		})
	}
	return staff, nil
}
