package steam

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agrelha/internal/ports"
)

func TestGetCollectionSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path == "/ISteamRemoteStorage/GetCollectionDetails/v1/" {
			if r.FormValue("publishedfileids[0]") != "104604903" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"response": {
					"result": 1,
					"resultcount": 1,
					"collectiondetails": [
						{
							"publishedfileid": "104604903",
							"result": 1,
							"children": [
								{"publishedfileid": "2419266395", "sortorder": 0, "filetype": 0},
								{"publishedfileid": "2419266500", "sortorder": 1, "filetype": 0}
							]
						}
					]
				}
			}`))
			return
		}
		if r.URL.Path == "/ISteamRemoteStorage/GetPublishedFileDetails/v1/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"response": {
					"result": 1,
					"resultcount": 1,
					"publishedfiledetails": [
						{
							"publishedfileid": "104604903",
							"result": 1,
							"title": "DarkRP Official Collection",
							"time_updated": 1696435200
						}
					]
				}
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := New("dummy-key", WithBaseURL(ts.URL))
	coll, err := client.GetCollection(context.Background(), "104604903")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if coll.ID != "104604903" {
		t.Errorf("expected ID 104604903, got %s", coll.ID)
	}
	if coll.Title != "DarkRP Official Collection" {
		t.Errorf("expected title 'DarkRP Official Collection', got %s", coll.Title)
	}
	if coll.ItemCount != 2 || len(coll.ItemIDs) != 2 {
		t.Errorf("expected 2 items, got count %d, slice %v", coll.ItemCount, coll.ItemIDs)
	}
	if coll.TimeUpdated != time.Unix(1696435200, 0).UTC() {
		t.Errorf("unexpected time_updated: %v", coll.TimeUpdated)
	}
}

func TestGetCollectionEmptyID(t *testing.T) {
	client := New("")
	_, err := client.GetCollection(context.Background(), "   ")
	if err == nil || !errors.Is(err, ports.ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound for empty id, got %v", err)
	}
}

func TestGetCollectionNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"response": {
				"result": 1,
				"resultcount": 1,
				"collectiondetails": [
					{
						"publishedfileid": "999999",
						"result": 9,
						"children": []
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := New("", WithBaseURL(ts.URL))
	_, err := client.GetCollection(context.Background(), "999999")
	if err == nil || !errors.Is(err, ports.ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestGetCollectionWithoutAPIKey(t *testing.T) {
	var receivedKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		receivedKey = r.FormValue("key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"response": {
				"result": 1,
				"resultcount": 1,
				"collectiondetails": [
					{
						"publishedfileid": "123",
						"result": 1,
						"children": []
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := New("", WithBaseURL(ts.URL))
	_, err := client.GetCollection(context.Background(), "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedKey != "" {
		t.Errorf("expected empty key, got %q", receivedKey)
	}
}

func TestGetCollectionWithAPIKey(t *testing.T) {
	var receivedKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		receivedKey = r.FormValue("key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"response": {
				"result": 1,
				"resultcount": 1,
				"collectiondetails": [
					{
						"publishedfileid": "123",
						"result": 1,
						"children": []
					}
				]
			}
		}`))
	}))
	defer ts.Close()

	client := New("secret-steam-key", WithBaseURL(ts.URL))
	_, err := client.GetCollection(context.Background(), "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedKey != "secret-steam-key" {
		t.Errorf("expected key 'secret-steam-key', got %q", receivedKey)
	}
}

func TestGetCollectionFallbackTitleOnDetailsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ISteamRemoteStorage/GetCollectionDetails/v1/" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"response": {
					"result": 1,
					"resultcount": 1,
					"collectiondetails": [
						{"publishedfileid": "456", "result": 1, "children": []}
					]
				}
			}`))
			return
		}
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := New("", WithBaseURL(ts.URL))
	coll, err := client.GetCollection(context.Background(), "456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if coll.Title != "Collection 456" {
		t.Errorf("expected fallback title 'Collection 456', got %q", coll.Title)
	}
}

func TestGetCollectionHTTP404(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	client := New("", WithBaseURL(ts.URL))
	_, err := client.GetCollection(context.Background(), "789")
	if err == nil || !errors.Is(err, ports.ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound on 404, got %v", err)
	}
}
