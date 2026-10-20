package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/jobinput"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// Attachment is a Feishu message resource reference, never an arbitrary URL.
type Attachment struct{ MessageID, Key, Kind, Name string }
type pendingAttachments struct {
	files   []Attachment
	expires time.Time
}

type AttachmentJobCreator interface {
	CreateFromFeishuWithAttachments(context.Context, string, string, string, string, string, string, []input.File) (string, error)
}

// AttachmentHistory reads persisted manifests, separately from pending resources.
type AttachmentHistory interface {
	RecentAttachmentJobs(context.Context, string, string, int) ([]AttachmentJob, error)
}

type AttachmentJob struct {
	JobID, EmployeeName string
	Files               []input.File
}

// ParseAttachments extracts post resources and standalone image/files.
// It visits the message body only, never forwarded cards or arbitrary JSON text.
func ParseAttachments(kind, raw, messageID string) []Attachment {
	var result []Attachment
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return nil
	}
	add := func(kind, key, name string) {
		if key != "" {
			result = append(result, Attachment{MessageID: messageID, Key: key, Kind: kind, Name: name})
		}
	}
	if kind == "image" || kind == "file" {
		var body struct {
			ImageKey string `json:"image_key"`
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
		}
		_ = json.Unmarshal([]byte(raw), &body)
		if kind == "image" {
			add("image", body.ImageKey, "image.png")
		} else {
			add("file", body.FileKey, body.FileName)
		}
	}
	if kind == "post" {
		type resource struct {
			Tag      string `json:"tag"`
			ImageKey string `json:"image_key"`
			FileKey  string `json:"file_key"`
			FileName string `json:"file_name"`
			IsFolder bool   `json:"is_folder"`
		}
		for _, body := range postResourceContainers(obj) {
			// Feishu can repeat the same resources in both row representations.
			for _, field := range []string{"content", "content_v2"} {
				var rows [][]resource
				_ = json.Unmarshal(body[field], &rows)
				for _, row := range rows {
					for _, el := range row {
						if el.Tag == "img" {
							add("image", el.ImageKey, "image.png")
						}
						if el.Tag == "file" && !el.IsFolder {
							add("file", el.FileKey, el.FileName)
						}
					}
				}
			}
			// Mixed image/text/file posts carry file cards beside the rows.
			var files []resource
			_ = json.Unmarshal(body["files"], &files)
			for _, file := range files {
				if !file.IsFolder {
					add("file", file.FileKey, file.FileName)
				}
			}
		}
	}
	return uniqueAttachments(result)
}

// Inspect only the message root and one localized post body. Never recurse into
// cards, forwarded messages or JSON embedded in text. Keep root-level files even
// when the text/image rows live in a locale envelope.
func postResourceContainers(root map[string]json.RawMessage) []map[string]json.RawMessage {
	out := []map[string]json.RawMessage{root}
	if root["content"] != nil || root["content_v2"] != nil {
		return out
	}
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range append([]string{"zh_cn", "en_us", "ja_jp"}, keys...) {
		if len(key) != 5 || key[2] != '_' || key[0] < 'a' || key[0] > 'z' || key[1] < 'a' || key[1] > 'z' || key[3] < 'a' || key[3] > 'z' || key[4] < 'a' || key[4] > 'z' {
			continue
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(root[key], &body) == nil && (body["content"] != nil || body["content_v2"] != nil || body["files"] != nil) {
			return append(out, body)
		}
	}
	return out
}

func uniqueAttachments(files []Attachment) []Attachment {
	seen := map[string]bool{}
	var result []Attachment
	for _, file := range files {
		key := file.MessageID + ":" + file.Key
		if !seen[key] {
			result = append(result, file)
			seen[key] = true
		}
	}
	return result
}

func (s *Service) attachmentKey(ev IncomingEvent) string { return ev.ChatID + "\x00" + ev.SenderOpenID }

func (s *Service) stageAttachments(ev IncomingEvent) error {
	if ev.ChatID == "" || ev.SenderOpenID == "" {
		return fmt.Errorf("无法确认附件发送者或会话")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingFiles == nil {
		s.pendingFiles = map[string]pendingAttachments{}
	}
	now := time.Now()
	for key, pending := range s.pendingFiles {
		if !now.Before(pending.expires) {
			delete(s.pendingFiles, key)
		}
	}
	key := s.attachmentKey(ev)
	files := uniqueAttachments(append(s.pendingFiles[key].files, ev.Attachments...))
	if len(files) > input.MaxFiles {
		return fmt.Errorf("待派单附件最多 %d 个, 请先发送任务", input.MaxFiles)
	}
	// Bound memory even when many users send files without submitting tasks.
	if _, exists := s.pendingFiles[key]; !exists && len(s.pendingFiles) >= 1000 {
		return fmt.Errorf("暂存附件过多, 请稍后重试或回复原附件消息派单")
	}
	s.pendingFiles[key] = pendingAttachments{files: files, expires: now.Add(15 * time.Minute)}
	return nil
}

func (s *Service) taskAttachments(ev IncomingEvent) []Attachment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	files := append([]Attachment{}, ev.Attachments...)
	if pending := s.pendingFiles[s.attachmentKey(ev)]; time.Now().Before(pending.expires) {
		files = append(files, pending.files...)
	}
	return uniqueAttachments(files)
}

func (s *Service) consumeAttachments(ev IncomingEvent, consumed []Attachment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.attachmentKey(ev)
	pending := s.pendingFiles[key]
	used := map[string]bool{}
	for _, file := range consumed {
		used[file.MessageID+":"+file.Key] = true
	}
	var remaining []Attachment
	for _, file := range pending.files {
		if !used[file.MessageID+":"+file.Key] {
			remaining = append(remaining, file)
		}
	}
	if len(remaining) == 0 {
		delete(s.pendingFiles, key)
	} else {
		pending.files = remaining
		s.pendingFiles[key] = pending
	}
}

func (s *Service) downloadAttachments(ctx context.Context, files []Attachment) ([]input.File, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > input.MaxFiles {
		return nil, fmt.Errorf("每条任务最多 %d 个附件", input.MaxFiles)
	}
	if s.InputFiles == nil {
		return nil, fmt.Errorf("附件存储服务未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var result []input.File
	for _, file := range files {
		f, err := s.downloadAttachment(ctx, file, s.InputFiles)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
		if err := input.Validate(result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) downloadAttachment(ctx context.Context, file Attachment, store *jobinput.Store) (input.File, error) {
	s.mu.RLock()
	client := s.larkClient
	s.mu.RUnlock()
	if client == nil {
		return input.File{}, fmt.Errorf("飞书客户端未就绪")
	}
	resp, err := client.Im.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().MessageId(file.MessageID).FileKey(file.Key).Type(file.Kind).Build())
	if err != nil {
		return input.File{}, fmt.Errorf("下载飞书附件失败, 请确认机器人有消息资源读取权限")
	}
	if resp == nil || !resp.Success() || resp.File == nil {
		return input.File{}, fmt.Errorf("下载飞书附件失败, 请检查资源权限或附件是否仍有效")
	}
	if closer, ok := resp.File.(io.Closer); ok {
		defer closer.Close()
	}
	name := file.Name
	if resp.FileName != "" {
		name = resp.FileName
	}
	f, err := store.Put(name, resp.File)
	if err != nil {
		return input.File{}, err
	}
	if file.Kind == "image" {
		ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[f.MIMEType]
		if ext == "" {
			return input.File{}, fmt.Errorf("无法识别图片格式, 请作为文件发送")
		}
		f.Name = "image-" + f.SHA256[:12] + ext
	}
	return f, nil
}

// FetchMessageContext also supports replying to an attachment with a task.
func (s *Service) FetchMessageContext(ctx context.Context, messageID, chatID string) (string, []Attachment, error) {
	s.mu.RLock()
	client := s.larkClient
	s.mu.RUnlock()
	if client == nil {
		return "", nil, fmt.Errorf("飞书客户端未就绪")
	}
	resp, err := client.Im.Message.Get(ctx, larkim.NewGetMessageReqBuilder().MessageId(messageID).Build())
	if err != nil || resp == nil || !resp.Success() || resp.Data == nil || len(resp.Data.Items) == 0 {
		return "", nil, fmt.Errorf("无法读取引用消息")
	}
	msg := resp.Data.Items[0]
	if msg.ChatId == nil || *msg.ChatId != chatID {
		return "", nil, fmt.Errorf("引用消息不属于当前会话")
	}
	if msg.Body == nil || msg.Body.Content == nil {
		return "", nil, nil
	}
	kind := ""
	if msg.MsgType != nil {
		kind = *msg.MsgType
	}
	text := ParseMessageBody(kind, *msg.Body.Content)
	if kind == "image" || kind == "file" {
		text = ""
	}
	return strings.TrimSpace(text), ParseAttachments(kind, *msg.Body.Content, messageID), nil
}
