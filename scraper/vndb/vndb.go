package vndb

import (
	"encoding/json"
	"maps"
	"net/http"
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
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const Name = "vndb"

var (
	SearchFields = []string{
		"id", "title", "titles.title", "titles.official", "image.url",
	}
	DetailFields = []string{
		"id", "title", "titles.title", "titles.official",
		"released", "languages", "platforms", "image.url", "description",
		"rating", "screenshots.url", "tags.name", "tags.aliases", "length_minutes",
		"developers.id", "developers.name", "developers.original", "developers.description",
		"developers.extlinks.url", "developers.extlinks.label",
		"staff.id", "staff.role", "staff.description", "staff.gender", "staff.name",
		"staff.original", "staff.extlinks.url", "staff.extlinks.label",
		"va.staff.id", "va.staff.description", "va.staff.gender", "va.staff.name",
		"va.staff.original", "va.staff.extlinks.url", "va.staff.extlinks.label",
		"va.character.id", "va.character.name", "va.character.original", "va.character.description",
		"va.character.image.url", "va.character.blood_type", "va.character.aliases", "va.character.vns.role",
		"va.character.height", "va.character.weight", "va.character.bust", "va.character.waist",
		"va.character.hips", "va.character.cup", "va.character.age", "va.character.birthday",
		"va.character.gender", "va.character.traits.name", "va.character.traits.description", "va.character.vns.spoiler",
		"va.note", "extlinks.url", "extlinks.label",
	}

	CharacterFields = []string{
		"id", "name", "original", "description",
		"image.url", "blood_type", "aliases", "vns.role",
		"height", "weight", "bust", "waist",
		"hips", "cup", "age", "birthday",
		"gender", "traits.name", "traits.description", "vns.spoiler",
	}
)

const (
	DefaultHeader_UserAgent = "dokidokikoi/izumi (https://github.com/dokidokikoi/izumi)"
)

var (
	VNDBSearchUri    = "https://api.vndb.org/kana/vn"
	VNDBCharacterUri = "https://api.vndb.org/kana/character"
)

type VNDB struct {
	sync.RWMutex
	name    string
	Headers map[string]string
	Proxy   string
	logger  *zap.Logger
	http    *httpx.Client
}

func NewVNDB(header map[string]string, proxy string) scraper.IGameScraper {
	if header == nil {
		header = map[string]string{}
	}
	return &VNDB{
		name:    Name,
		Headers: header,
		Proxy:   proxy,
		logger:  zaplog.L().Named(Name),
		http:    httpx.New(Name, proxy),
	}
}

func (v *VNDB) SetHeader(header map[string]string) {
	v.Lock()
	v.Headers = header
	v.Unlock()
}

func (v *VNDB) SetProxy(proxy string) {
	v.Lock()
	v.Proxy = proxy
	v.Unlock()
	v.http.SetProxy(proxy)
}

func (v *VNDB) SearchGame(keyword string, page int) ([]*scraper.SearchItem, error) {
	data, err := v.DoReq(http.MethodPost, VNDBSearchUri, nil, map[string]any{
		"filters": []any{
			"search", "=", keyword,
		},
		"fields":  strings.Join(SearchFields, ","),
		"results": 20,
		"page":    page,
	})
	if err != nil {
		zaplog.L().Error("vndb search error", zap.Error(err))
		return nil, err
	}
	var resp BaseResponse[VN]
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, err
	}
	items := make([]*scraper.SearchItem, 0, len(resp.Result))
	for _, val := range resp.Result {
		items = append(items, &scraper.SearchItem{
			Name: func() string {
				for _, t := range val.Titles {
					if t.Official {
						return t.Title
					}
				}
				return val.Title
			}(),
			Cover:       val.Image.Url,
			URl:         val.ID,
			ScraperName: v.GetName(),
		})
	}
	return items, nil
}

func (v *VNDB) GetGameItem(uri string) (*scraper.GameItem, error) {
	group := errgroup.Group{}
	var extraCs []handler.CharacterVo
	group.Go(func() error {
		var err error
		extraCs, err = v.FetchCharacters(uri)
		if err != nil {
			zaplog.L().With(zap.Error(err)).Error("FetchCharacters")
		}
		return nil
	})
	data, err := v.DoReq(http.MethodPost, VNDBSearchUri, nil, map[string]any{
		"filters": []any{
			"id", "=", uri,
		},
		"fields": strings.Join(DetailFields, ","),
	})
	if err != nil {
		zaplog.L().Error("vndb get item error", zap.Error(err))
		return nil, err
	}
	var resp BaseResponse[VN]
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if len(resp.Result) == 0 {
		return nil, nil
	}
	res := resp.Result[0]
	characters := buildCharacters(res)
	charSet := tools.NewSet[string]()
	for _, c := range characters {
		charSet.Add(c.Name)
	}

	group.Wait()
	for _, c := range extraCs {
		if !charSet.Has(c.Name) {
			charSet.Add(c.Name)
			characters = append(characters, c)
		}
	}
	item := &scraper.GameItem{
		GameVo: handler.GameVo{
			RelIDs: []string{Name + ":" + res.ID},
			Name: func() string {
				for _, t := range res.Titles {
					if t.Official {
						return t.Title
					}
				}
				return res.Title
			}(),
			Alias: func() []string {
				alias := []string{}
				for _, t := range res.Titles {
					if !t.Official {
						alias = append(alias, t.Title)
					} else {
						alias = append(alias, res.Title)
					}
				}
				return alias
			}(),
			Cover: res.Image.Url,
			Images: func() []string {
				images := make([]string, 0, len(res.Screenshots))
				for _, s := range res.Screenshots {
					images = append(images, s.Url)
				}
				return images
			}(),
			Brands: func() []*model.Brand {
				var brands []*model.Brand
				for _, dev := range res.Developers {
					links := []model.Link{}
					for _, extLink := range dev.ExtLinks {
						links = append(links, Link2Link(extLink))
					}
					brands = append(brands, &model.Brand{
						Name:  dev.Name,
						Links: links,
					})
				}
				return brands
			}(),
			IssueDate: tools.Str2Time(res.Released),
			Story:     res.Description,
			Tags: func() []*model.Tag {
				tags := []*model.Tag{}
				// for _, tag := range res.Tags {
				// 	tags = append(tags, &model.Tag{
				// 		Name: tag.Name,
				// 	})
				// }
				return tags
			}(),
			Staff:      buildStaff(res),
			Characters: characters,
			Links: func() []model.Link {
				links := []model.Link{{Name: Name, Url: "https://vndb.org/" + uri, Type: model.LinkTypeSource}}
				for _, item := range res.ExtLinks {
					links = append(links, Link2Link(item))
				}
				return links
			}(),
		},
		ScraperName: v.GetName(),
	}

	// 一次性批量下载所有图片：封面 + 截图 + 角色封面，避免逐角色串行。
	urls := make([]string, 0, 2+len(res.Screenshots)+len(item.Characters))
	urls = append(urls, res.Image.Url)
	for _, s := range res.Screenshots {
		urls = append(urls, s.Url)
	}
	for _, c := range item.Characters {
		urls = append(urls, c.Cover)
	}
	saved := utils.SaveBunchTmpFiles(func(u string) ([]byte, error) {
		return v.DoReq(http.MethodGet, u, nil, nil)
	}, urls)
	paths := make(map[string]string, len(saved))
	for _, f := range saved {
		paths[f.URL] = f.Path
	}
	item.Cover = strings.TrimPrefix(paths[res.Image.Url], config.TmpDir)
	images := make([]string, 0, len(res.Screenshots))
	for _, s := range res.Screenshots {
		if p, ok := paths[s.Url]; ok {
			images = append(images, strings.TrimPrefix(p, config.TmpDir))
		}
	}
	item.Images = images
	for i := range item.Characters {
		item.Characters[i].Cover = strings.TrimPrefix(paths[item.Characters[i].Cover], config.TmpDir)
	}
	return item, nil
}

func buildStaff(res VN) []handler.StaffVo {
	staffs := []handler.StaffVo{}
	staffM := map[string]int{}
	for _, staff := range res.Staff {
		idx, ok := staffM[staff.ID]
		if !ok {
			staffs = append(staffs, Staff2Staff(staff))
			staffM[staff.ID] = len(staffs) - 1
		} else if r := TransfPRelation(staff.Role); r != model.PRelationUnknown {
			staffs[idx].Relation = append(staffs[idx].Relation, r)
		}
	}
	for _, va := range res.VA {
		idx, ok := staffM[va.Staff.ID]
		if !ok {
			staff := Staff2Staff(va.Staff)
			staff.Relation = append(staff.Relation, model.PRelationCV)
			staffs = append(staffs, staff)
			staffM[va.Staff.ID] = len(staffs) - 1
		} else {
			staffs[idx].Relation = append(staffs[idx].Relation, model.PRelationCV)
		}
	}
	return staffs
}

func buildCharacters(res VN) []handler.CharacterVo {
	roleM := map[string][]handler.CharacterVo{}
	characterM := map[string]struct{}{}
	for _, va := range res.VA {
		char := va.Character
		if _, ok := characterM[char.ID]; ok {
			continue
		}
		characterM[char.ID] = struct{}{}
		role := ""
		for _, v := range char.VNS {
			if v.ID == res.ID {
				role = v.Role
				break
			}
		}

		c := buildCharacter(char, res.ID)
		c.CV = Staff2Staff(va.Staff)
		roleM[role] = append(roleM[role], c)
	}

	characters := roleM["primary"]
	characters = append(characters, roleM["main"]...)
	delete(roleM, "primary")
	delete(roleM, "main")

	charM := map[string]int{}
	for i, c := range characters {
		charM[c.Name] = i
	}

	for _, cs := range roleM {
		for _, c := range cs {
			if idx, ok := charM[c.Name]; ok {
				characters[idx].Images = tools.NewSet(append(characters[idx].Images, c.Images...)...).Slice()
			} else {
				charM[c.Name] = len(characters)
				characters = append(characters, c)
			}
		}
	}
	return characters
}

func buildCharacter(char Character, vnID string) handler.CharacterVo {
	role := ""
	for _, vn := range char.VNS {
		if vn.ID == vnID {
			role = char.VNS[0].Role
		}
	}
	return handler.CharacterVo{
		RelIDs: []string{Name + ":" + char.ID},
		Name: func() string {
			if char.Original != "" {
				return char.Original
			}
			return char.Name
		}(),
		Alias: func() []string {
			if char.Original != "" {
				return append(char.Aliases, char.Name)
			}
			return char.Aliases
		}(),
		Gender: func() model.Gender {
			if len(char.Gender) > 1 {
				return TransfGender(char.Gender[1])
			} else if len(char.Gender) > 0 {
				return TransfGender(char.Gender[0])
			}
			return model.UnKnown
		}(),
		Summary: char.Description,
		Cover:   char.Image.Url,
		PersonalInfo: model.PersonalInfo{
			Age: char.Age,
			Birthday: func() [2]int {
				if len(char.Birthday) > 1 {
					return [2]int{char.Birthday[0], char.Birthday[1]}
				} else if len(char.Birthday) > 0 {
					return [2]int{char.Birthday[0], 0}
				}
				return [2]int{0, 0}
			}(),
			BloodType: char.BloodType,
			Bust:      char.Bust,
			Cup:       char.Cup,
			Height:    char.Height,
			Weight:    char.Weight,
			Waist:     char.Waist,
			Hips:      char.Hips,
			Traits: func() []model.Trait {
				traits := []model.Trait{}
				for _, trait := range char.Traits {
					traits = append(traits, model.Trait{
						Name:        trait.Name,
						Description: trait.Description,
					})
				}
				return traits
			}(),
		},
		Rlation: TransfCRelation(role),
	}
}

func (v *VNDB) DoReq(method, uri string, header map[string]string, body any) ([]byte, error) {
	h := map[string]string{}
	v.RLock()
	maps.Copy(h, v.Headers)
	v.RUnlock()
	maps.Copy(h, header)

	rsp, err := v.http.Do(method, uri, h, body)
	if err != nil {
		return nil, err
	}
	if rsp.StatusCode()/100 != 2 {
		return nil, errors.Errorf("status code: %d, body: %s", rsp.StatusCode(), rsp.String())
	}
	return rsp.Bytes(), nil
}

func (v *VNDB) FetchCharacters(vnID string) ([]handler.CharacterVo, error) {
	data, err := v.DoReq(http.MethodPost, VNDBCharacterUri, nil, map[string]any{
		"filters": []any{
			"vn", "=", []string{"id", "=", vnID},
		},
		"fields":  strings.Join(CharacterFields, ","),
		"results": 100,
	})
	if err != nil {
		return nil, err
	}
	var resp BaseResponse[Character]
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return nil, err
	}
	var cs []handler.CharacterVo
	for _, c := range resp.Result {
		cs = append(cs, buildCharacter(c, vnID))
	}
	return cs, nil
}

func (v *VNDB) GetName() string {
	return v.name
}
