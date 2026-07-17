package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mangahub/internal/auth"
	"mangahub/internal/db"
	"mangahub/internal/models"
	"mangahub/internal/tcp"
)

type Server struct {
	router    *gin.Engine
	db        *db.DB
	jwtSecret string
	tcpServer *tcp.Server
}

func New(database *db.DB, jwtSecret string, tcpSrv *tcp.Server) *Server {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})
	s := &Server{router: r, db: database, jwtSecret: jwtSecret, tcpServer: tcpSrv}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.GET("/health", s.health)
	s.router.POST("/auth/register", s.register)
	s.router.POST("/auth/login", s.login)
	s.router.GET("/manga", s.searchManga)
	s.router.GET("/manga/:id", s.getManga)
	auth := s.router.Group("/")
	auth.Use(s.jwt())
	auth.GET("/users/me", s.getMe)
	auth.GET("/users/library", s.getLibrary)
	auth.POST("/users/library", s.addToLibrary)
	auth.PUT("/users/progress", s.updateProgress)
}

func (s *Server) Run(addr string) error {
	log.Printf("[HTTP] API server on %s", addr)
	return s.router.Run(addr)
}

func (s *Server) Handler() http.Handler { return s.router }

func genID(prefix string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return fmt.Sprintf("%s_%x", prefix, b)
}

func (s *Server) jwt() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(401, models.ErrorResponse{Error: "authorization required"})
			return
		}
		claims, err := auth.ValidateToken(strings.TrimPrefix(h, "Bearer "), s.jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(401, models.ErrorResponse{Error: "invalid token"})
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}

func (s *Server) health(c *gin.Context) {
	count, _ := s.db.CountManga()
	c.JSON(200, gin.H{"status": "ok", "manga_count": count, "time": time.Now().Format(time.RFC3339)})
}

func (s *Server) register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.ErrorResponse{Error: err.Error()})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(500, models.ErrorResponse{Error: "hash error"})
		return
	}
	u := &models.User{ID: genID("usr"), Username: req.Username, Email: req.Email, PasswordHash: hash}
	if err := s.db.CreateUser(u); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			c.JSON(409, models.ErrorResponse{Error: "username or email taken"})
			return
		}
		c.JSON(500, models.ErrorResponse{Error: "db error"})
		return
	}
	c.JSON(201, gin.H{"user_id": u.ID, "username": u.Username, "message": "account created"})
}

func (s *Server) login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.ErrorResponse{Error: err.Error()})
		return
	}
	u, err := s.db.GetUserByUsername(req.Username)
	if err != nil || !auth.CheckPassword(req.Password, u.PasswordHash) {
		c.JSON(401, models.ErrorResponse{Error: "invalid credentials"})
		return
	}
	token, _ := auth.GenerateToken(u.ID, u.Username, s.jwtSecret)
	c.JSON(200, models.LoginResponse{Token: token, Username: u.Username, UserID: u.ID})
}

func (s *Server) getMe(c *gin.Context) {
	u, err := s.db.GetUserByID(c.GetString("user_id"))
	if err != nil {
		c.JSON(404, models.ErrorResponse{Error: "user not found"})
		return
	}
	c.JSON(200, gin.H{"user_id": u.ID, "username": u.Username, "email": u.Email})
}

func (s *Server) searchManga(c *gin.Context) {
	limit := 20
	if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 {
		limit = l
	}
	results, err := s.db.SearchManga(c.Query("q"), c.Query("genre"), c.Query("status"), limit)
	if err != nil {
		c.JSON(500, models.ErrorResponse{Error: "search failed"})
		return
	}
	if results == nil {
		results = []*models.Manga{}
	}
	c.JSON(200, gin.H{"results": results, "total": len(results)})
}

func (s *Server) getManga(c *gin.Context) {
	m, err := s.db.GetMangaByID(c.Param("id"))
	if err != nil {
		c.JSON(404, models.ErrorResponse{Error: "manga not found: " + c.Param("id")})
		return
	}
	c.JSON(200, m)
}

func (s *Server) getLibrary(c *gin.Context) {
	lib, err := s.db.GetLibrary(c.GetString("user_id"))
	if err != nil {
		c.JSON(500, models.ErrorResponse{Error: "db error"})
		return
	}
	if lib == nil {
		lib = []*models.UserProgress{}
	}
	c.JSON(200, gin.H{"library": lib, "total": len(lib)})
}

func (s *Server) addToLibrary(c *gin.Context) {
	var req models.AddLibraryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.ErrorResponse{Error: err.Error()})
		return
	}
	if _, err := s.db.GetMangaByID(req.MangaID); err != nil {
		c.JSON(404, models.ErrorResponse{Error: "manga not found"})
		return
	}
	s.db.UpsertProgress(&models.UserProgress{
		UserID: c.GetString("user_id"), MangaID: req.MangaID, Status: req.Status,
	})
	c.JSON(201, gin.H{"message": "added", "manga_id": req.MangaID})
}

func (s *Server) updateProgress(c *gin.Context) {
	var req models.UpdateProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, models.ErrorResponse{Error: err.Error()})
		return
	}
	userID := c.GetString("user_id")
	st := req.Status
	if st == "" {
		st = "reading"
	}
	s.db.UpsertProgress(&models.UserProgress{
		UserID: userID, MangaID: req.MangaID, CurrentChapter: req.Chapter, Status: st,
	})
	if s.tcpServer != nil {
		data, _ := json.Marshal(models.ProgressUpdate{
			UserID: userID, MangaID: req.MangaID,
			Chapter: req.Chapter, Timestamp: time.Now().Unix(),
		})
		s.tcpServer.Broadcast(data)
		log.Printf("[HTTP] Broadcast progress: %s ch%d", req.MangaID, req.Chapter)
	}
	c.JSON(200, gin.H{"message": "updated", "manga_id": req.MangaID, "chapter": req.Chapter})
}
