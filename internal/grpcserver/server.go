package grpcserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"mangahub/internal/db"
	"mangahub/internal/models"
	mangapb "mangahub/proto"
)

type mangaServiceImpl struct {
	mangapb.UnimplementedMangaServiceServer
	db *db.DB
}

func (s *mangaServiceImpl) GetManga(ctx context.Context, req *mangapb.GetMangaRequest) (*mangapb.MangaResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "manga id required")
	}
	m, err := s.db.GetMangaByID(req.Id)
	if err != nil {
		return &mangapb.MangaResponse{Error: "not found: " + req.Id}, nil
	}
	return &mangapb.MangaResponse{
		Id: m.ID, Title: m.Title, Author: m.Author, Genres: m.Genres,
		Status: m.Status, TotalChapters: int32(m.TotalChapters), Description: m.Description,
	}, nil
}

func (s *mangaServiceImpl) SearchManga(ctx context.Context, req *mangapb.SearchRequest) (*mangapb.SearchResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}
	results, err := s.db.SearchManga(req.Query, req.Genre, req.Status, limit)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &mangapb.SearchResponse{Total: int32(len(results))}
	for _, m := range results {
		resp.Results = append(resp.Results, &mangapb.MangaResponse{
			Id: m.ID, Title: m.Title, Author: m.Author, Genres: m.Genres,
			Status: m.Status, TotalChapters: int32(m.TotalChapters),
		})
	}
	return resp, nil
}

func (s *mangaServiceImpl) UpdateProgress(ctx context.Context, req *mangapb.ProgressRequest) (*mangapb.ProgressResponse, error) {
	if req.UserId == "" || req.MangaId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and manga_id required")
	}
	st := req.Status
	if st == "" {
		st = "reading"
	}
	if err := s.db.UpsertProgress(&models.UserProgress{
		UserID: req.UserId, MangaID: req.MangaId,
		CurrentChapter: int(req.Chapter), Status: st,
	}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	log.Printf("[gRPC] UpdateProgress user=%s manga=%s ch=%d", req.UserId, req.MangaId, req.Chapter)
	return &mangapb.ProgressResponse{
		Success: true,
		Message: fmt.Sprintf("updated %s to chapter %d", req.MangaId, req.Chapter),
	}, nil
}

type Server struct {
	port   string
	db     *db.DB
	server *grpc.Server
}

func New(port string, database *db.DB) *Server {
	return &Server{port: port, db: database}
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", ":"+s.port)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	s.server = grpc.NewServer(grpc.UnaryInterceptor(loggingInterceptor))
	mangapb.RegisterMangaServiceServer(s.server, &mangaServiceImpl{db: s.db})
	log.Printf("[gRPC] Service listening on :%s", s.port)
	return s.server.Serve(lis)
}

func (s *Server) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
}

func loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	log.Printf("[gRPC] %s took %v err=%v", info.FullMethod, time.Since(start).Round(time.Millisecond), err)
	return resp, err
}

type Client struct {
	conn *grpc.ClientConn
	c    mangapb.MangaServiceClient
}

func NewClient(addr string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}
	return &Client{conn: conn, c: mangapb.NewMangaServiceClient(conn)}, nil
}

func (c *Client) Close() { c.conn.Close() }

func (c *Client) GetManga(ctx context.Context, id string) (*mangapb.MangaResponse, error) {
	return c.c.GetManga(ctx, &mangapb.GetMangaRequest{Id: id})
}

func (c *Client) SearchManga(ctx context.Context, query string) (*mangapb.SearchResponse, error) {
	return c.c.SearchManga(ctx, &mangapb.SearchRequest{Query: query, Limit: 10})
}

func (c *Client) UpdateProgress(ctx context.Context, userID, mangaID string, chapter int) (*mangapb.ProgressResponse, error) {
	return c.c.UpdateProgress(ctx, &mangapb.ProgressRequest{UserId: userID, MangaId: mangaID, Chapter: int32(chapter)})
}
