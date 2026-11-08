package http_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/saransh1220/blueprint-audio/internal/gateway/middleware"
	analyticsDomain "github.com/saransh1220/blueprint-audio/internal/modules/analytics/domain"
	"github.com/saransh1220/blueprint-audio/internal/modules/catalog/domain"
	catalogHTTP "github.com/saransh1220/blueprint-audio/internal/modules/catalog/interfaces/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeRedis is a minimal RESP2 server: every GET returns the configured value
// (or nil), SET/DEL are recorded.
type fakeRedis struct {
	ln    net.Listener
	mu    sync.Mutex
	value string
	cmds  []string
}

func newFakeRedis(t *testing.T, value string) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	f := &fakeRedis{ln: ln, value: value}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeRedis) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "*") {
			return
		}
		n, _ := strconv.Atoi(strings.TrimSpace(line[1:]))
		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			size, _ := strconv.Atoi(strings.TrimSpace(l[1:]))
			buf := make([]byte, size+2)
			if _, err := io.ReadFull(r, buf); err != nil {
				return
			}
			args = append(args, string(buf[:size]))
		}
		cmd := strings.ToUpper(args[0])
		f.mu.Lock()
		f.cmds = append(f.cmds, cmd)
		val := f.value
		f.mu.Unlock()
		switch cmd {
		case "GET":
			if val == "" {
				_, _ = c.Write([]byte("$-1\r\n"))
			} else {
				_, _ = fmt.Fprintf(c, "$%d\r\n%s\r\n", len(val), val)
			}
		case "SET", "CLIENT", "SELECT":
			_, _ = c.Write([]byte("+OK\r\n"))
		case "DEL":
			_, _ = c.Write([]byte(":1\r\n"))
		default:
			_, _ = c.Write([]byte("-ERR unknown command\r\n"))
		}
	}
}

func (f *fakeRedis) count(cmd string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cmds {
		if c == cmd {
			n++
		}
	}
	return n
}

func newHandlerWithRedis(addr string) (*catalogHTTP.SpecHandler, *mockSpecService, *mockFileService, *mockAnalyticsService) {
	specSvc := new(mockSpecService)
	fileSvc := new(mockFileService)
	analyticsSvc := new(mockAnalyticsService)
	rdb := redis.NewClient(&redis.Options{Addr: addr, Protocol: 2, DisableIdentity: true, MaxRetries: -1})
	return catalogHTTP.NewSpecHandler(specSvc, fileSvc, analyticsSvc, new(mockNotificationService), rdb), specSvc, fileSvc, analyticsSvc
}

func withUser(r *http.Request, id uuid.UUID) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.ContextKeyUserId, id))
}

func getReq(id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/specs/"+id, nil)
	r.SetPathValue("id", id)
	return r
}

func TestSpecHandler_Get_ShortCodeAndSlugLookups(t *testing.T) {
	t.Run("short code with authenticated analytics", func(t *testing.T) {
		h, specSvc, _, analyticsSvc, _ := newHandler()
		specID, viewer := uuid.New(), uuid.New()
		specSvc.On("GetSpecByShortCode", mock.Anything, "abcd1234").
			Return(&domain.Spec{ID: specID, Title: "Short", ProcessingStatus: domain.ProcessingStatusProcessing}, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, &viewer).
			Return(&analyticsDomain.PublicAnalytics{PlayCount: 7, FavoriteCount: 2, IsFavorited: true}, nil).Once()

		w := httptest.NewRecorder()
		h.Get(w, withUser(getReq("abcd1234"), viewer))

		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "MISS", w.Header().Get("X-Cache"))
		assert.Contains(t, w.Body.String(), `"title":"Short"`)
		assert.Contains(t, w.Body.String(), `"play_count":7`)
		specSvc.AssertExpectations(t)
	})

	t.Run("slug lookup error returns 500", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("GetSpecBySlug", mock.Anything, "some-long-slug").Return(nil, errors.New("db")).Once()
		w := httptest.NewRecorder()
		h.Get(w, getReq("some-long-slug"))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "internal server error")
	})

	t.Run("sanitize presigns image and preview, tolerates presign failure", func(t *testing.T) {
		h, specSvc, fileSvc, analyticsSvc, _ := newHandler()
		specID := uuid.New()
		spec := &domain.Spec{
			ID: specID, Title: "P", PreviewUrl: "http://b/p.mp3", ImageUrl: "http://b/i.jpg",
			ProcessingStatus: domain.ProcessingStatusProcessing,
		}
		specSvc.On("GetSpec", mock.Anything, specID).Return(spec, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, (*uuid.UUID)(nil)).Return(nil, errors.New("down")).Once()
		fileSvc.On("GetKeyFromUrl", "http://b/p.mp3").Return("p.mp3", nil).Once()
		fileSvc.On("GetPresignedDownloadURL", mock.Anything, "p.mp3", "P", time.Hour).Return("", errors.New("sign")).Once()
		fileSvc.On("GetKeyFromUrl", "http://b/i.jpg").Return("i.jpg", nil).Once()
		fileSvc.On("GetPresignedURL", mock.Anything, "i.jpg", time.Hour).Return("http://signed/i.jpg", nil).Once()

		w := httptest.NewRecorder()
		h.Get(w, getReq(specID.String()))

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "http://b/p.mp3")
		assert.Contains(t, w.Body.String(), "http://signed/i.jpg")
		fileSvc.AssertExpectations(t)
	})
}

func TestSpecHandler_Get_CacheBehaviour(t *testing.T) {
	t.Run("cache hit uses fresh analytics for each viewer", func(t *testing.T) {
		fr := newFakeRedis(t, `{"processing_status":"completed","title":"Cached","analytics":{"is_favorited":true}}`)
		h, specSvc, _, analyticsSvc := newHandlerWithRedis(fr.ln.Addr().String())
		specID := uuid.New()
		viewer := uuid.New()
		specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{ID: specID}, nil).Times(3)
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, &viewer).
			Return(&analyticsDomain.PublicAnalytics{IsFavorited: true}, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, &viewer).
			Return(&analyticsDomain.PublicAnalytics{IsFavorited: false}, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, (*uuid.UUID)(nil)).
			Return(nil, errors.New("down")).Once()

		for _, want := range []bool{true, false} {
			w := httptest.NewRecorder()
			h.Get(w, withUser(getReq(specID.String()), viewer))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "HIT", w.Header().Get("X-Cache"))
			assert.Equal(t, "private, no-store", w.Header().Get("Cache-Control"))
			var response catalogHTTP.SpecResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			assert.Equal(t, "Cached", response.Title)
			require.NotNil(t, response.Analytics)
			assert.Equal(t, want, response.Analytics.IsFavorited)
		}

		w := httptest.NewRecorder()
		h.Get(w, getReq(specID.String()))
		assert.NotContains(t, w.Body.String(), `"analytics"`)
	})

	t.Run("cached processing entry is invalidated and rebuilt", func(t *testing.T) {
		fr := newFakeRedis(t, `{"processing_status":"processing"}`)
		h, specSvc, _, analyticsSvc := newHandlerWithRedis(fr.ln.Addr().String())
		specID := uuid.New()
		specSvc.On("GetSpec", mock.Anything, specID).
			Return(&domain.Spec{ID: specID, Title: "Fresh", ProcessingStatus: domain.ProcessingStatusProcessing}, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, (*uuid.UUID)(nil)).Return(nil, errors.New("down")).Once()

		w := httptest.NewRecorder()
		h.Get(w, getReq(specID.String()))

		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "MISS", w.Header().Get("X-Cache"))
		assert.Contains(t, w.Body.String(), "Fresh")
		assert.Equal(t, 3, fr.count("DEL"), "base key plus INR and USD variants are deleted")
	})

	t.Run("completed spec is written to cache", func(t *testing.T) {
		fr := newFakeRedis(t, "")
		h, specSvc, _, analyticsSvc := newHandlerWithRedis(fr.ln.Addr().String())
		specID := uuid.New()
		specSvc.On("GetSpec", mock.Anything, specID).
			Return(&domain.Spec{ID: specID, Title: "Done", ProcessingStatus: domain.ProcessingStatusCompleted}, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, (*uuid.UUID)(nil)).Return(nil, errors.New("down")).Once()

		w := httptest.NewRecorder()
		h.Get(w, getReq(specID.String()))

		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "MISS", w.Header().Get("X-Cache"))
		assert.Eventually(t, func() bool { return fr.count("SET") == 1 }, 2*time.Second, 10*time.Millisecond)
	})

	t.Run("cache write failure does not affect the response", func(t *testing.T) {
		h, specSvc, _, analyticsSvc, _ := newHandler()
		specID := uuid.New()
		specSvc.On("GetSpec", mock.Anything, specID).
			Return(&domain.Spec{ID: specID, Title: "Done", ProcessingStatus: domain.ProcessingStatusCompleted}, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, (*uuid.UUID)(nil)).Return(nil, errors.New("down")).Once()

		w := httptest.NewRecorder()
		h.Get(w, getReq(specID.String()))
		assert.Equal(t, http.StatusOK, w.Code)
		time.Sleep(100 * time.Millisecond) // let the async cache write fail and log
	})
}

func TestSpecHandler_ListAndHome_Branches(t *testing.T) {
	t.Run("list parses moods and instruments and reports one page when empty", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("ListSpecs", mock.Anything, mock.MatchedBy(func(f domain.SpecFilter) bool {
			return len(f.Moods) == 2 && f.Moods[1] == "sad" && len(f.Instruments) == 1 && f.Instruments[0] == "piano"
		})).Return([]domain.Spec{}, 0, nil).Once()

		w := httptest.NewRecorder()
		h.List(w, httptest.NewRequest(http.MethodGet, "/specs?moods=dark,sad&instruments=piano", nil))

		require.Equal(t, http.StatusOK, w.Code)
		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		var md map[string]float64
		require.NoError(t, json.Unmarshal(raw["metadata"], &md))
		assert.Equal(t, float64(1), md["total_pages"])
		assert.Equal(t, float64(0), md["total"])
	})

	t.Run("list service error", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("ListSpecs", mock.Anything, mock.Anything).Return(nil, 0, errors.New("db")).Once()
		w := httptest.NewRecorder()
		h.List(w, httptest.NewRequest(http.MethodGet, "/specs", nil))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("home service error", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("GetHome", mock.Anything, mock.MatchedBy(func(p domain.HomepageParams) bool {
			return p.Limit == 5 && p.Period == "week" && len(p.Sections) == 2 && p.Sections[1] == "new"
		})).Return(nil, errors.New("db")).Once()
		w := httptest.NewRecorder()
		h.Home(w, httptest.NewRequest(http.MethodGet, "/home?limit=5&period=week&sections=trending,%20new", nil))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "internal server error")
	})
}

func TestSpecHandler_Delete_MoreBranches(t *testing.T) {
	specID, owner := uuid.New(), uuid.New()
	delReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodDelete, "/specs/"+specID.String(), nil)
		r.SetPathValue("id", specID.String())
		return withUser(r, owner)
	}

	t.Run("invalid id", func(t *testing.T) {
		h, _, _, _, _ := newHandler()
		r := httptest.NewRequest(http.MethodDelete, "/specs/x", nil)
		r.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		h.Delete(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	for _, status := range []domain.ProcessingStatus{domain.ProcessingStatusPending, domain.ProcessingStatusProcessing} {
		t.Run("conflict while "+string(status), func(t *testing.T) {
			h, specSvc, _, _, _ := newHandler()
			specSvc.On("GetSpec", mock.Anything, specID).
				Return(&domain.Spec{ID: specID, ProducerID: owner, ProcessingStatus: status}, nil).Once()
			w := httptest.NewRecorder()
			h.Delete(w, delReq())
			assert.Equal(t, http.StatusConflict, w.Code)
			assert.Contains(t, w.Body.String(), domain.ErrSpecProcessing.Error())
		})
	}

	t.Run("service reports processing", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{ID: specID, ProducerID: owner}, nil).Once()
		specSvc.On("DeleteSpec", mock.Anything, specID, owner).Return(domain.ErrSpecProcessing).Once()
		w := httptest.NewRecorder()
		h.Delete(w, delReq())
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("service generic error", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{ID: specID, ProducerID: owner}, nil).Once()
		specSvc.On("DeleteSpec", mock.Anything, specID, owner).Return(errors.New("boom")).Once()
		w := httptest.NewRecorder()
		h.Delete(w, delReq())
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("success removes every stored file and tolerates key errors", func(t *testing.T) {
		h, specSvc, fileSvc, _, _ := newHandler()
		wav, stems := "http://b/m.wav", "http://b/s.zip"
		specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{
			ID: specID, ProducerID: owner, ImageUrl: "http://b/i.jpg", PreviewUrl: "http://b/bad",
			WavUrl: &wav, StemsUrl: &stems,
		}, nil).Once()
		specSvc.On("DeleteSpec", mock.Anything, specID, owner).Return(nil).Once()
		fileSvc.On("GetKeyFromUrl", "http://b/i.jpg").Return("i.jpg", nil).Once()
		fileSvc.On("GetKeyFromUrl", "http://b/bad").Return("", errors.New("bad url")).Once()
		fileSvc.On("GetKeyFromUrl", wav).Return("m.wav", nil).Once()
		fileSvc.On("GetKeyFromUrl", stems).Return("s.zip", nil).Once()
		fileSvc.On("Delete", mock.Anything, "i.jpg").Return(nil).Once()
		fileSvc.On("Delete", mock.Anything, "m.wav").Return(nil).Once()
		fileSvc.On("Delete", mock.Anything, "s.zip").Return(errors.New("ignored")).Once()

		w := httptest.NewRecorder()
		h.Delete(w, delReq())

		assert.Equal(t, http.StatusNoContent, w.Code)
		fileSvc.AssertExpectations(t)
		fileSvc.AssertNotCalled(t, "Delete", mock.Anything, "")
	})
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return buf.Bytes()
}

func updateImageReq(t *testing.T, specID, producer uuid.UUID, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	require.NoError(t, mw.WriteField("metadata", `{"title":"New"}`))
	part, err := mw.CreateFormFile("image", "c.png")
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	r := httptest.NewRequest(http.MethodPatch, "/specs/"+specID.String(), &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.SetPathValue("id", specID.String())
	return withUser(r, producer)
}

func TestSpecHandler_Update_MoreBranches(t *testing.T) {
	specID, producer := uuid.New(), uuid.New()

	t.Run("invalid id and unauthorized", func(t *testing.T) {
		h, _, _, _, _ := newHandler()
		r := httptest.NewRequest(http.MethodPatch, "/specs/x", nil)
		r.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		h.Update(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		r = httptest.NewRequest(http.MethodPatch, "/specs/"+specID.String(), nil)
		r.SetPathValue("id", specID.String())
		w = httptest.NewRecorder()
		h.Update(w, r)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("forbidden for non owner", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{ID: specID, ProducerID: uuid.New()}, nil).Once()
		w := httptest.NewRecorder()
		h.Update(w, updateImageReq(t, specID, producer, pngBytes(t)))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("conflict while processing", func(t *testing.T) {
		h, specSvc, _, _, _ := newHandler()
		specSvc.On("GetSpec", mock.Anything, specID).
			Return(&domain.Spec{ID: specID, ProducerID: producer, ProcessingStatus: domain.ProcessingStatusProcessing}, nil).Once()
		w := httptest.NewRecorder()
		h.Update(w, updateImageReq(t, specID, producer, pngBytes(t)))
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("image replaced and old image removed", func(t *testing.T) {
		h, specSvc, fileSvc, _, _ := newHandler()
		specSvc.On("GetSpec", mock.Anything, specID).
			Return(&domain.Spec{ID: specID, ProducerID: producer, ImageUrl: "http://b/old.jpg"}, nil).Once()
		fileSvc.On("UploadWithKey", mock.Anything, mock.Anything, mock.MatchedBy(func(k string) bool {
			return strings.HasPrefix(k, "images/") && strings.HasSuffix(k, ".jpg")
		}), "image/jpeg").Return("http://b/new.jpg", nil).Once()
		specSvc.On("UpdateSpec", mock.Anything, mock.MatchedBy(func(s *domain.Spec) bool {
			return s.Title == "New" && s.ImageUrl == "http://b/new.jpg"
		}), producer).Return(nil).Once()
		fileSvc.On("GetKeyFromUrl", "http://b/old.jpg").Return("old.jpg", nil).Once()
		fileSvc.On("Delete", mock.Anything, "old.jpg").Return(nil).Once()
		fileSvc.On("GetKeyFromUrl", "http://b/new.jpg").Return("", errors.New("skip")).Once()

		w := httptest.NewRecorder()
		h.Update(w, updateImageReq(t, specID, producer, pngBytes(t)))

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"title":"New"`)
		fileSvc.AssertExpectations(t)
	})

	t.Run("upload failure", func(t *testing.T) {
		h, specSvc, fileSvc, _, _ := newHandler()
		specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{ID: specID, ProducerID: producer}, nil).Once()
		fileSvc.On("UploadWithKey", mock.Anything, mock.Anything, mock.Anything, "image/jpeg").Return("", errors.New("s3 down")).Once()
		w := httptest.NewRecorder()
		h.Update(w, updateImageReq(t, specID, producer, pngBytes(t)))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "failed to upload image: s3 down")
	})

	for name, tc := range map[string]struct {
		err  error
		code int
	}{
		"processing during save": {domain.ErrSpecProcessing, http.StatusConflict},
		"generic save error":     {errors.New("db"), http.StatusInternalServerError},
	} {
		t.Run("uploaded image is rolled back on "+name, func(t *testing.T) {
			h, specSvc, fileSvc, _, _ := newHandler()
			specSvc.On("GetSpec", mock.Anything, specID).Return(&domain.Spec{ID: specID, ProducerID: producer}, nil).Once()
			fileSvc.On("UploadWithKey", mock.Anything, mock.Anything, mock.Anything, "image/jpeg").Return("http://b/new.jpg", nil).Once()
			specSvc.On("UpdateSpec", mock.Anything, mock.Anything, producer).Return(tc.err).Once()
			fileSvc.On("Delete", mock.Anything, mock.MatchedBy(func(k string) bool { return strings.HasPrefix(k, "images/") })).Return(nil).Once()

			w := httptest.NewRecorder()
			h.Update(w, updateImageReq(t, specID, producer, pngBytes(t)))

			assert.Equal(t, tc.code, w.Code)
			fileSvc.AssertExpectations(t)
		})
	}
}

func TestSpecHandler_GetUserSpecs_MoreBranches(t *testing.T) {
	t.Run("defaults page, includes viewer analytics and one page when total is zero", func(t *testing.T) {
		h, specSvc, _, analyticsSvc, _ := newHandler()
		userID, viewer, specID := uuid.New(), uuid.New(), uuid.New()
		specSvc.On("GetUserSpecs", mock.Anything, userID, 1, 20).
			Return([]domain.Spec{{ID: specID, Title: "Mine"}}, 0, nil).Once()
		analyticsSvc.On("GetPublicAnalytics", mock.Anything, specID, &viewer).
			Return(&analyticsDomain.PublicAnalytics{PlayCount: 3}, nil).Once()

		r := httptest.NewRequest(http.MethodGet, "/users/x/specs?page=0", nil)
		r.SetPathValue("id", userID.String())
		w := httptest.NewRecorder()
		h.GetUserSpecs(w, withUser(r, viewer))

		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"title":"Mine"`)
		assert.Contains(t, w.Body.String(), `"play_count":3`)
		assert.Contains(t, w.Body.String(), `"total_pages":1`)
	})

	t.Run("invalid user id", func(t *testing.T) {
		h, _, _, _, _ := newHandler()
		r := httptest.NewRequest(http.MethodGet, "/users/x/specs", nil)
		r.SetPathValue("id", "x")
		w := httptest.NewRecorder()
		h.GetUserSpecs(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid user id")
	})
}

func TestSpecHandler_DownloadFree_TrackingFailureIsNotFatal(t *testing.T) {
	h, specSvc, fileSvc, analyticsSvc, _ := newHandler()
	specID := uuid.New()
	tracked := make(chan struct{})
	spec := &domain.Spec{ID: specID, Title: "T", FreeMp3Enabled: true, PreviewUrl: "http://b/p.mp3"}
	specSvc.On("GetSpec", mock.Anything, specID).Return(spec, nil).Once()
	analyticsSvc.On("TrackFreeDownload", mock.Anything, specID).
		Run(func(mock.Arguments) { close(tracked) }).Return(errors.New("analytics down")).Once()
	fileSvc.On("GetKeyFromUrl", spec.PreviewUrl).Return("p.mp3", nil).Once()
	fileSvc.On("GetPresignedDownloadURL", mock.Anything, "p.mp3", "T.mp3", time.Hour).Return("http://signed", nil).Once()

	r := httptest.NewRequest(http.MethodPost, "/specs/x/download-free", nil)
	r.SetPathValue("id", specID.String())
	w := httptest.NewRecorder()
	h.DownloadFree(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"url":"http://signed"`)
	select {
	case <-tracked:
	case <-time.After(2 * time.Second):
		t.Fatal("free download was never tracked")
	}
	time.Sleep(50 * time.Millisecond) // allow the goroutine to log the failure
}
