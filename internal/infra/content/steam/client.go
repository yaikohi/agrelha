package steam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agrelha/internal/domain"
	"agrelha/internal/ports"
)

var _ ports.WorkshopResolver = (*Client)(nil)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(u, "/")
	}
}

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.http = httpClient
	}
}

func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: "https://api.steampowered.com",
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type collectionDetailsResponse struct {
	Response struct {
		Result            int `json:"result"`
		ResultCount       int `json:"resultcount"`
		CollectionDetails []struct {
			PublishedFileID string `json:"publishedfileid"`
			Result          int    `json:"result"`
			Children        []struct {
				PublishedFileID string `json:"publishedfileid"`
				SortOrder       int    `json:"sortorder"`
				FileType        int    `json:"filetype"`
			} `json:"children"`
		} `json:"collectiondetails"`
	} `json:"response"`
}

type publishedFileDetailsResponse struct {
	Response struct {
		Result               int `json:"result"`
		ResultCount          int `json:"resultcount"`
		PublishedFileDetails []struct {
			PublishedFileID string `json:"publishedfileid"`
			Result          int    `json:"result"`
			Title           string `json:"title"`
			TimeUpdated     int64  `json:"time_updated"`
			ConsumerAppID   int    `json:"consumer_app_id"`
		} `json:"publishedfiledetails"`
	} `json:"response"`
}

func (c *Client) postForm(ctx context.Context, endpoint string, values url.Values, out any) error {
	if c.apiKey != "" && values.Get("key") == "" {
		values.Set("key", c.apiKey)
	}
	reqURL := fmt.Sprintf("%s/%s", c.baseURL, strings.TrimPrefix(endpoint, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", reqURL, ports.ErrPackageNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: status %d: %s", reqURL, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) GetCollection(ctx context.Context, id string) (domain.WorkshopCollection, error) {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return domain.WorkshopCollection{}, fmt.Errorf("collection id is required: %w", ports.ErrPackageNotFound)
	}

	collValues := url.Values{}
	collValues.Set("collectioncount", "1")
	collValues.Set("publishedfileids[0]", cleanID)

	var collResp collectionDetailsResponse
	if err := c.postForm(ctx, "ISteamRemoteStorage/GetCollectionDetails/v1/", collValues, &collResp); err != nil {
		return domain.WorkshopCollection{}, err
	}

	if len(collResp.Response.CollectionDetails) == 0 {
		return domain.WorkshopCollection{}, fmt.Errorf("collection %s: %w", cleanID, ports.ErrPackageNotFound)
	}

	collDetail := collResp.Response.CollectionDetails[0]
	if collDetail.Result != 1 {
		return domain.WorkshopCollection{}, fmt.Errorf("collection %s result %d: %w", cleanID, collDetail.Result, ports.ErrPackageNotFound)
	}

	itemIDs := make([]string, 0, len(collDetail.Children))
	for _, child := range collDetail.Children {
		if child.PublishedFileID != "" {
			itemIDs = append(itemIDs, child.PublishedFileID)
		}
	}

	fileValues := url.Values{}
	fileValues.Set("itemcount", "1")
	fileValues.Set("publishedfileids[0]", cleanID)

	var fileResp publishedFileDetailsResponse
	title := ""
	var timeUpdated time.Time

	if err := c.postForm(ctx, "ISteamRemoteStorage/GetPublishedFileDetails/v1/", fileValues, &fileResp); err == nil {
		if len(fileResp.Response.PublishedFileDetails) > 0 {
			fd := fileResp.Response.PublishedFileDetails[0]
			if fd.Result == 1 {
				title = fd.Title
				if fd.TimeUpdated > 0 {
					timeUpdated = time.Unix(fd.TimeUpdated, 0).UTC()
				}
			}
		}
	}

	if title == "" {
		title = fmt.Sprintf("Collection %s", cleanID)
	}

	return domain.WorkshopCollection{
		ID:          cleanID,
		Title:       title,
		ItemCount:   len(itemIDs),
		ItemIDs:     itemIDs,
		TimeUpdated: timeUpdated,
	}, nil
}
