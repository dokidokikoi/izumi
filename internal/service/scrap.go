package service

import (
	"context"
	"encoding/json"
	"fmt"
	"izumi/constant"
	"izumi/db"
	"izumi/model"
	"izumi/scraper/event"
	"izumi/utils"
	"time"

	zaplog "github.com/dokidokikoi/go-common/log/zap"
	"github.com/dokidokikoi/go-common/notice"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type IScrap interface {
	Scrap(ctx context.Context, requestID string, objs []model.ScrapObj, afterHook ...func(scraperName string, success bool)) (*errgroup.Group, error)
}

var _ IScrap = (*scrap)(nil)

type scrap struct {
	store db.IStore
}

func (s *scrap) Scrap(ctx context.Context, requestID string, objs []model.ScrapObj, afterHook ...func(scraperName string, success bool)) (*errgroup.Group, error) {
	group := errgroup.Group{}
	group.SetLimit(10)
	for _, req := range objs {
		_, err := db.GetStore().Task().Get(ctx, &model.Task{RequestID: requestID, Param: req.Url}, nil)
		if err == nil {
			continue
		}
		s, ok := event.GameScraperMap[req.Name]
		if !ok {
			continue
		}
		group.Go(func() error {
			var err error
			defer func() {
				success := true
				if err != nil {
					success = false
				}
				notice.HubIns.SendBroadcast("", notice.NoticeResponse{
					Rid:     requestID,
					Event:   utils.ConcatEvent(constant.TOPIC_SCRAPER, constant.EVENT_SCRAPER_DETAIL),
					Success: success,
					Data: map[string]any{
						"name": s.GetName(),
					},
				})
				if len(afterHook) > 0 {
					afterHook[0](s.GetName(), success)
				}
			}()

			task := &model.Task{
				RequestID:   requestID,
				ScraperName: req.Name,
				Type:        model.TaskTypeDetail,
				Param:       req.Url,
				Status:      model.TaskStatusWait,
				StartAt:     time.Now(),
			}
			err = db.GetStore().Task().Create(ctx, task, nil)
			if err != nil {
				return errors.Wrap(err, "创建任务失败")
			}
			defer func() {
				err := db.GetStore().Task().Update(ctx, task, nil)
				if err != nil {
					zaplog.L().Debug("更新任务失败", zap.Error(err))
					return
				}
			}()
			item, err := s.GetGameItem(req.Url)
			task.EndAt = time.Now()
			task.Duration = int64(task.EndAt.Sub(task.StartAt).Seconds())
			if err != nil {
				task.Status = model.TaskStatusFailed
				return fmt.Errorf("获取详情失败. scraper name: %s, url: %s. err: %w", req.Name, req.Url, err)
			}

			data, err := json.Marshal(item)
			if err != nil {
				task.Status = model.TaskStatusFailed
				return errors.Wrap(err, "刮削失败")
			}
			task.Result = string(data)
			task.Status = model.TaskStatusSucceed

			return nil
		})
	}
	return &group, nil
}

func NewScrap(store db.IStore) *scrap {
	return &scrap{store: store}
}
