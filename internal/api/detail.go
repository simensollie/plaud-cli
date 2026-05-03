package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Segment is the canonical-on-disk transcript segment shape, mapped from
// Plaud's wire format. Mirrors archive.Segment's JSON tags by design: cmd/plaud
// converts between the two via struct conversion at the api/archive boundary.
type Segment struct {
	Speaker         string `json:"speaker"`
	OriginalSpeaker string `json:"original_speaker,omitempty"`
	StartMs         int64  `json:"start_ms"`
	EndMs           int64  `json:"end_ms"`
	Text            string `json:"text"`
}

// RecordingDetail is the parsed result of /file/detail/{id} after mapping to
// canonical shapes. Empty Segments / empty Summary indicate "not yet ready"
// per F-19; the caller decides what to do.
type RecordingDetail struct {
	ID        string
	Title     string
	Language  string
	Segments  []Segment
	Summary   string
	IsTrash   bool
	StartTime time.Time
	Duration  time.Duration
}

// rawSegment is Plaud's wire shape for one transcript segment. Mapped to
// Segment at ingest. Times are milliseconds despite the lack of an `_ms`
// suffix (verified empirically; see notes.md 2026-05-02 entry).
type rawSegment struct {
	StartTime       int64  `json:"start_time"`
	EndTime         int64  `json:"end_time"`
	Content         string `json:"content"`
	Speaker         string `json:"speaker"`
	OriginalSpeaker string `json:"original_speaker"`
}

type rawContentEntry struct {
	DataID     string `json:"data_id"`
	DataType   string `json:"data_type"`
	TaskStatus int    `json:"task_status"`
	DataLink   string `json:"data_link"`
}

type rawPreDownload struct {
	DataID      string `json:"data_id"`
	DataContent string `json:"data_content"`
}

type rawAIContentHeader struct {
	LanguageCode string `json:"language_code"`
}

type rawExtraData struct {
	AIContentHeader rawAIContentHeader `json:"aiContentHeader"`
}

type rawDetailData struct {
	FileID                 string            `json:"file_id"`
	FileName               string            `json:"file_name"`
	IsTrash                bool              `json:"is_trash"`
	StartTime              int64             `json:"start_time"`
	Duration               int64             `json:"duration"`
	ContentList            []rawContentEntry `json:"content_list"`
	PreDownloadContentList []rawPreDownload  `json:"pre_download_content_list"`
	ExtraData              rawExtraData      `json:"extra_data"`
}

type detailResponse struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

// Detail fetches /file/detail/{id} and returns canonical mapped data. When
// the recording has no transcript yet (no `transaction` entry in
// content_list, or task_status != 1), Segments is nil without error. Same for
// Summary. F-10: HTTP 401 returns ErrUnauthorized.
func (c *Client) Detail(ctx context.Context, id string) (*RecordingDetail, error) {
	url := fmt.Sprintf("%s/file/detail/%s", c.baseURL, id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building /file/detail request: %w", err)
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("calling /file/detail: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("reading /file/detail body: %w", readErr)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: http %d", ErrAPIError, resp.StatusCode)
	}

	var env detailResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decoding /file/detail: %w", err)
	}
	if env.Status != 0 {
		return nil, fmt.Errorf("%w: status=%d msg=%q", ErrAPIError, env.Status, env.Msg)
	}

	var data rawDetailData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return nil, fmt.Errorf("decoding /file/detail data: %w", err)
	}

	out := &RecordingDetail{
		ID:        data.FileID,
		Title:     data.FileName,
		Language:  data.ExtraData.AIContentHeader.LanguageCode,
		IsTrash:   data.IsTrash,
		StartTime: time.UnixMilli(data.StartTime).UTC(),
		Duration:  time.Duration(data.Duration) * time.Millisecond,
	}

	segs, err := c.resolveTranscript(ctx, data.ContentList)
	if err != nil {
		return nil, err
	}
	out.Segments = segs

	summary, err := c.resolveSummary(ctx, data.ContentList, data.PreDownloadContentList)
	if err != nil {
		return nil, err
	}
	out.Summary = summary

	return out, nil
}

func (c *Client) resolveTranscript(ctx context.Context, contentList []rawContentEntry) ([]Segment, error) {
	for _, entry := range contentList {
		if entry.DataType != "transaction" || entry.TaskStatus != 1 || entry.DataLink == "" {
			continue
		}
		body, err := c.fetchSignedJSON(ctx, entry.DataLink)
		if err != nil {
			return nil, fmt.Errorf("fetching transcript artifact: %w", err)
		}
		var raws []rawSegment
		if err := json.Unmarshal(body, &raws); err != nil {
			return nil, fmt.Errorf("decoding transcript JSON: %w", err)
		}
		segs := make([]Segment, len(raws))
		for i, r := range raws {
			seg := Segment{
				Speaker: r.Speaker,
				StartMs: r.StartTime,
				EndMs:   r.EndTime,
				Text:    r.Content,
			}
			if r.OriginalSpeaker != r.Speaker {
				seg.OriginalSpeaker = r.OriginalSpeaker
			}
			segs[i] = seg
		}
		return segs, nil
	}
	return nil, nil
}

func (c *Client) resolveSummary(ctx context.Context, contentList []rawContentEntry, preDownload []rawPreDownload) (string, error) {
	// Find the auto_sum_note entry (if any) so we can match its data_id
	// against the inlined pre_download_content_list.
	var summaryEntry *rawContentEntry
	for i := range contentList {
		if contentList[i].DataType == "auto_sum_note" && contentList[i].TaskStatus == 1 {
			summaryEntry = &contentList[i]
			break
		}
	}

	if summaryEntry != nil {
		for _, pre := range preDownload {
			if pre.DataID == summaryEntry.DataID && pre.DataContent != "" {
				return pre.DataContent, nil
			}
		}
		if summaryEntry.DataLink != "" {
			body, err := c.fetchSignedJSON(ctx, summaryEntry.DataLink)
			if err != nil {
				return "", fmt.Errorf("fetching summary artifact: %w", err)
			}
			return string(body), nil
		}
	}

	// Fallback: some captures inline the summary even when no auto_sum_note
	// content_list entry exists yet. Match by data_id prefix.
	for _, pre := range preDownload {
		if pre.DataContent != "" && len(pre.DataID) >= 8 && pre.DataID[:8] == "auto_sum" {
			return pre.DataContent, nil
		}
	}

	return "", nil
}
