package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/ripple/internal/config"
	"github.com/jasper0507/ripple/internal/model"
	"github.com/jasper0507/ripple/internal/response"
	"github.com/jasper0507/ripple/internal/testenv"
	"gorm.io/gorm"
)

// 库名和 Redis DB 与其他测试包错开，见 testenv 包注释。
const (
	testDatabase = "ripple_test_app"
	testRedisDB  = 12

	testPassword = "Passw0rd!"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func TestAuthFlow(t *testing.T) {
	h, _ := newTestServer(t)

	signup := func(username, password, confirm string) any {
		return map[string]string{"username": username, "password": password, "confirm_password": confirm}
	}
	for _, tt := range []struct {
		name       string
		body       any
		wantStatus int
		wantCode   response.Code
	}{
		{name: "密码缺少特殊符号", body: signup("alice01", "Password1", "Password1"), wantStatus: 400, wantCode: response.CodeInvalidParams},
		{name: "两次密码不一致", body: signup("alice01", testPassword, "Passw0rd?"), wantStatus: 400, wantCode: response.CodeInvalidParams},
		{name: "注册成功", body: signup("alice01", testPassword, testPassword), wantStatus: 201, wantCode: response.CodeOK},
		{name: "用户名已存在", body: signup("alice01", testPassword, testPassword), wantStatus: 409, wantCode: response.CodeUsernameExists},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(h, http.MethodPost, "/api/v1/signup", "", tt.body)
			expect(t, w, tt.wantStatus, tt.wantCode)
		})
	}

	// 用户不存在与密码错误必须返回相同结果，避免枚举用户名。
	for _, user := range []string{"alice01", "nobody01"} {
		w := serve(h, http.MethodPost, "/api/v1/login", "", map[string]string{"username": user, "password": "Wr0ngpass!"})
		expect(t, w, 401, response.CodeInvalidCredentials)
	}

	w := serve(h, http.MethodPost, "/api/v1/login", "", map[string]string{"username": "alice01", "password": testPassword})
	var login struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	expectData(t, w, 200, &login)
	if login.AccessToken == "" || login.TokenType != "Bearer" {
		t.Fatalf("登录响应 = %+v", login)
	}
	first := refreshCookie(t, w)
	if !first.HttpOnly || first.Path != "/" || first.MaxAge <= 0 {
		t.Fatalf("refresh cookie 属性不正确: %+v", first)
	}

	expect(t, serve(h, http.MethodPost, "/api/v1/refresh", "", nil), 401, response.CodeUnauthorized)

	w = serve(h, http.MethodPost, "/api/v1/refresh", "", nil, first)
	var refreshed struct {
		AccessToken string `json:"access_token"`
	}
	expectData(t, w, 200, &refreshed)
	second := refreshCookie(t, w)
	if refreshed.AccessToken == "" || second.Value == first.Value {
		t.Fatalf("刷新后应换发新令牌: token = %q, cookie = %+v", refreshed.AccessToken, second)
	}
	// 旧 refresh token 只能使用一次。
	expect(t, serve(h, http.MethodPost, "/api/v1/refresh", "", nil, first), 401, response.CodeUnauthorized)

	w = serve(h, http.MethodPost, "/api/v1/logout", "", nil, second)
	expect(t, w, 204, "")
	if cleared := refreshCookie(t, w); cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("退出后应清除 cookie: %+v", cleared)
	}
	expect(t, serve(h, http.MethodPost, "/api/v1/refresh", "", nil, second), 401, response.CodeUnauthorized)
}

func TestJWTAuth(t *testing.T) {
	h, _ := newTestServer(t)
	token := registerAndLogin(t, h, "alice01")

	for _, tt := range []struct {
		name   string
		header string
	}{
		{name: "缺少 Authorization"},
		{name: "非 Bearer 方案", header: "Token " + token},
		{name: "缺少令牌", header: "Bearer"},
		{name: "多余字段", header: "Bearer " + token + " extra"},
		{name: "无效令牌", header: "Bearer invalid"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest(http.MethodGet, "/api/v1/posts/1/vote", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			expect(t, w, 401, response.CodeUnauthorized)
		})
	}

	// 方案名不区分大小写；通过鉴权后才会走到帖子不存在的分支。
	req := newRequest(http.MethodGet, "/api/v1/posts/1/vote", nil)
	req.Header.Set("Authorization", "bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	expect(t, w, 404, response.CodePostNotFound)
}

func TestPostAPI(t *testing.T) {
	h, db := newTestServer(t)
	author := registerAndLogin(t, h, "author01")
	other := registerAndLogin(t, h, "other01")
	communityID := seedCommunity(t, db)

	newPost := map[string]any{"title": "Go 并发", "content": "正文", "community_id": communityID}
	expect(t, serve(h, http.MethodPost, "/api/v1/posts", author, map[string]any{"content": "正文", "community_id": communityID}), 400, response.CodeInvalidParams)
	expect(t, serve(h, http.MethodPost, "/api/v1/posts", author, map[string]any{"title": "标题", "content": "正文", "community_id": communityID + 1000}), 404, response.CodeCommunityNotFound)
	postID := createdID(t, serve(h, http.MethodPost, "/api/v1/posts", author, newPost))
	postPath := fmt.Sprintf("/api/v1/posts/%d", postID)

	for _, tt := range []struct {
		name       string
		path       string
		body       string
		wantStatus int
		wantCode   response.Code
	}{
		{name: "缺少 direction", path: postPath + "/vote", body: `{}`, wantStatus: 400, wantCode: response.CodeInvalidParams},
		{name: "direction 越界", path: postPath + "/vote", body: `{"direction":2}`, wantStatus: 400, wantCode: response.CodeInvalidParams},
		// direction 为指针类型，0 不能被 required 当成缺失。
		{name: "取消投票", path: postPath + "/vote", body: `{"direction":0}`, wantStatus: 200, wantCode: response.CodeOK},
		{name: "赞成", path: postPath + "/vote", body: `{"direction":1}`, wantStatus: 200, wantCode: response.CodeOK},
		{name: "帖子 ID 非数字", path: "/api/v1/posts/abc/vote", body: `{"direction":1}`, wantStatus: 400, wantCode: response.CodeInvalidParams},
		{name: "帖子 ID 为零", path: "/api/v1/posts/0/vote", body: `{"direction":1}`, wantStatus: 400, wantCode: response.CodeInvalidParams},
		{name: "帖子不存在", path: fmt.Sprintf("/api/v1/posts/%d/vote", postID+1000), body: `{"direction":1}`, wantStatus: 404, wantCode: response.CodePostNotFound},
	} {
		t.Run("投票/"+tt.name, func(t *testing.T) {
			w := serve(h, http.MethodPut, tt.path, author, json.RawMessage(tt.body))
			expect(t, w, tt.wantStatus, tt.wantCode)
		})
	}
	var vote struct {
		Direction int8 `json:"direction"`
	}
	expectData(t, serve(h, http.MethodGet, postPath+"/vote", author, nil), 200, &vote)
	if vote.Direction != 1 {
		t.Fatalf("投票方向 = %d, want 1", vote.Direction)
	}

	for _, query := range []string{"order=newest", "page=0", "page=abc", "page=9223372036854775807", "community_id=0"} {
		t.Run("列表/"+query, func(t *testing.T) {
			expect(t, serve(h, http.MethodGet, "/api/v1/posts?"+query, "", nil), 400, response.CodeInvalidParams)
		})
	}
	expect(t, serve(h, http.MethodGet, "/api/v1/posts?order=hot&page=1", "", nil), 200, response.CodeOK)

	expect(t, serve(h, http.MethodDelete, postPath, other, nil), 403, response.CodeForbidden)
	expect(t, serve(h, http.MethodDelete, postPath, author, nil), 204, "")
	expect(t, serve(h, http.MethodGet, postPath, "", nil), 404, response.CodePostNotFound)
}

func TestCommentAPI(t *testing.T) {
	h, db := newTestServer(t)
	author := registerAndLogin(t, h, "author01")
	other := registerAndLogin(t, h, "other01")
	communityID := seedCommunity(t, db)
	newPost := func() uint {
		body := map[string]any{"title": "讨论帖", "content": "正文", "community_id": communityID}
		return createdID(t, serve(h, http.MethodPost, "/api/v1/posts", author, body))
	}
	postID, otherPostID := newPost(), newPost()
	commentsPath := func(id uint) string { return fmt.Sprintf("/api/v1/posts/%d/comments", id) }

	commentID := createdID(t, serve(h, http.MethodPost, commentsPath(otherPostID), author, map[string]string{"content": "另一帖的评论"}))
	for _, tt := range []struct {
		name       string
		postID     uint
		body       any
		wantStatus int
		wantCode   response.Code
	}{
		{name: "内容为空", postID: postID, body: map[string]string{"content": ""}, wantStatus: 400, wantCode: response.CodeInvalidParams},
		{name: "帖子不存在", postID: postID + 1000, body: map[string]string{"content": "评论"}, wantStatus: 404, wantCode: response.CodePostNotFound},
		{name: "跨帖回复", postID: postID, body: map[string]any{"content": "回复", "reply_to_comment_id": commentID}, wantStatus: 400, wantCode: response.CodeInvalidParams},
	} {
		t.Run("创建/"+tt.name, func(t *testing.T) {
			expect(t, serve(h, http.MethodPost, commentsPath(tt.postID), author, tt.body), tt.wantStatus, tt.wantCode)
		})
	}

	expect(t, serve(h, http.MethodGet, commentsPath(otherPostID)+"?page=0", "", nil), 400, response.CodeInvalidParams)
	var list struct {
		Total int64             `json:"total"`
		Items []json.RawMessage `json:"items"`
	}
	expectData(t, serve(h, http.MethodGet, commentsPath(otherPostID)+"?page=2", "", nil), 200, &list)
	// 超出数据范围的页必须返回空数组，而不是 null。
	if list.Total != 1 || list.Items == nil || len(list.Items) != 0 {
		t.Fatalf("第二页 = %+v, want 空数组、共 1 条", list)
	}

	commentPath := fmt.Sprintf("/api/v1/comments/%d", commentID)
	expect(t, serve(h, http.MethodDelete, commentPath, other, nil), 403, response.CodeForbidden)
	expect(t, serve(h, http.MethodDelete, commentPath, author, nil), 204, "")
	expect(t, serve(h, http.MethodDelete, commentPath, author, nil), 404, response.CodeCommentNotFound)
}

func newTestServer(t *testing.T) (http.Handler, *gorm.DB) {
	t.Helper()

	db := testenv.MySQL(t, testDatabase)
	a, err := New(db, testenv.Redis(t, testRedisDB), &config.Config{
		Auth: config.AuthConfig{
			Secret:          "12345678901234567890123456789012",
			Issuer:          "ripple-test",
			AccessTokenTTL:  time.Minute,
			RefreshTokenTTL: time.Hour,
		},
	})
	if err != nil {
		t.Fatalf("组装应用失败: %v", err)
	}

	return a.Router, db
}

func newRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			panic(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func serve(h http.Handler, method, path, token string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := newRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// expect 校验状态码和统一响应中的业务码；wantCode 为空表示无响应体。
func expect(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, wantCode response.Code) {
	t.Helper()

	if w.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, wantStatus, w.Body)
	}
	if wantCode == "" {
		if w.Body.Len() != 0 {
			t.Fatalf("body = %s, want 空", w.Body)
		}
		return
	}
	var resp response.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body = %s", err, w.Body)
	}
	if resp.Code != wantCode || resp.Message != wantCode.Message() {
		t.Fatalf("响应 = %+v, want code %s", resp, wantCode)
	}
}

func expectData(t *testing.T, w *httptest.ResponseRecorder, wantStatus int, data any) {
	t.Helper()

	expect(t, w, wantStatus, response.CodeOK)
	resp := response.Response{Data: data}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应数据失败: %v, body = %s", err, w.Body)
	}
}

func createdID(t *testing.T, w *httptest.ResponseRecorder) uint {
	t.Helper()

	var data struct {
		ID uint `json:"id"`
	}
	expectData(t, w, 201, &data)
	return data.ID
}

func refreshCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "refresh_token" {
			return cookie
		}
	}
	t.Fatalf("响应缺少 refresh_token cookie: %v", w.Header())
	return nil
}

func registerAndLogin(t *testing.T, h http.Handler, username string) string {
	t.Helper()

	body := map[string]string{"username": username, "password": testPassword, "confirm_password": testPassword}
	expect(t, serve(h, http.MethodPost, "/api/v1/signup", "", body), 201, response.CodeOK)

	var login struct {
		AccessToken string `json:"access_token"`
	}
	expectData(t, serve(h, http.MethodPost, "/api/v1/login", "", body), 200, &login)
	return login.AccessToken
}

func seedCommunity(t *testing.T, db *gorm.DB) uint {
	t.Helper()

	community := &model.Community{Name: "Go", Introduction: "Go"}
	if err := db.Create(community).Error; err != nil {
		t.Fatalf("创建社区失败: %v", err)
	}
	return community.ID
}
