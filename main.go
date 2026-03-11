package main

import (
	"errors"
	"fmt"
	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// VideoInfo 结构体用于存储视频文件的信息
type VideoInfo struct {
	ID        int    `json:"id" gorm:"primary_key;column:id"`
	Filename  string `json:"filename" gorm:"column:name;not null"`
	FilePath  string `json:"filepath" gorm:"column:path;not null"`
	Filesize  string `json:"filesize" gorm:"column:size;not null"`
	CreatedAt string `json:"created_at" gorm:"column:create_time"`
	UpdatedAt string `json:"updated_at" gorm:"column:update_time"`
}

// 自定义表名
func (VideoInfo) TableName() string {
	return "app01_video_info"
}

// Pagination 分页结构体
type Pagination struct {
	Page     int    `form:"page,default=1" binding:"min=1"`
	PageSize int    `form:"page_size,default=50" binding:"min=1,max=100"`
	Keyword  string `form:"keyword"`
}

// Response 统一响应结构
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// 初始化数据库连接
func initDB() *gorm.DB {
	db, err := gorm.Open("sqlite3", "db.sqlite3")
	if err != nil {
		log.Fatalf("Failed to connect database: %v", err)
	}

	// 设置连接池
	db.DB().SetMaxIdleConns(10)
	db.DB().SetMaxOpenConns(100)
	db.DB().SetConnMaxLifetime(time.Hour)

	// 自动迁移
	db.AutoMigrate(&VideoInfo{})

	return db
}

func formatTime(t interface{}) string {
	var str string

	// 1. 获取字符串值
	switch v := t.(type) {
	case string:
		str = v
	default:
		// 如果不是字符串，尝试转为字符串
		str = fmt.Sprintf("%v", v)
	}

	if str == "" || str == "<nil>" {
		return "" // 空值直接返回空，不显示
	}

	// 2. 尝试解析字符串为时间
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999", // 带毫秒
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}

	for _, format := range formats {
		tm, err := time.Parse(format, str)
		if err == nil {
			// return tm.Format("2006-01-02 15:04:05") // 输出统一格式
			return tm.Format("2006-01-02")
		}
	}

	// 【重要】如果解析失败，为了不让页面空白，直接返回原始字符串
	// 这样至少能看到数据，方便排查
	log.Printf("Warning: Time format '%s' not recognized, returning raw string.", str)
	return str
}

// loadTemplates 加载模板文件
func loadTemplates(templatesDir string) multitemplate.Renderer {
	r := multitemplate.NewRenderer()

	funcMap := template.FuncMap{
		"add":        func(a, b int) int { return a + b },
		"sub":        func(a, b int) int { return a - b },
		"formatDate": formatTime,
		"mod":        func(i, j int) bool { return i%j == 0 },
		"contains":   func(s, substr string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(substr)) },
	}

	// 加载布局模板
	layouts, err := filepath.Glob(templatesDir + "/layouts/*.tmpl")
	if err != nil {
		log.Fatalf("Failed to load layout templates: %v", err)
	}

	// 加载包含模板
	includes, err := filepath.Glob(templatesDir + "/includes/*.tmpl")
	if err != nil {
		log.Fatalf("Failed to load include templates: %v", err)
	}

	// 为每个include模板创建组合
	for _, include := range includes {
		files := append(layouts, include)
		templateName := filepath.Base(include)
		r.AddFromFilesFuncs(templateName, funcMap, files...)
	}

	return r
}

// GetVideos 获取视频列表（带分页和搜索）
func GetVideos(c *gin.Context) {
	var pagination Pagination

	// 绑定查询参数，默认值已在结构体tag中定义
	if err := c.ShouldBindQuery(&pagination); err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    http.StatusBadRequest,
			Message: "Invalid query parameters: " + err.Error(),
		})
		return
	}

	// 计算偏移量
	offset := (pagination.Page - 1) * pagination.PageSize

	// 构建查询条件
	dbQuery := DB.Model(&VideoInfo{})
	if pagination.Keyword != "" {
		searchTerm := "%" + pagination.Keyword + "%"
		dbQuery = dbQuery.Where("name LIKE ? OR path LIKE ?", searchTerm, searchTerm)
	}

	// 查询视频列表
	var videos []VideoInfo
	result := dbQuery.Offset(offset).Limit(pagination.PageSize).Order("id DESC").Find(&videos)
	if result.Error != nil {
		log.Printf("Database query error: %v", result.Error)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    http.StatusInternalServerError,
			Message: "Database query failed",
		})
		return
	}

	// 查询总记录数
	var total int64
	totalResult := DB.Model(&VideoInfo{})
	if pagination.Keyword != "" {
		searchTerm := "%" + pagination.Keyword + "%"
		totalResult = totalResult.Where("name LIKE ? OR path LIKE ?", searchTerm, searchTerm)
	}
	totalResult.Count(&total)

	// 计算总页数
	totalPages := int((total + int64(pagination.PageSize) - 1) / int64(pagination.PageSize))

	// 返回HTML响应
	c.HTML(http.StatusOK, "home.tmpl", gin.H{
		"videos":      videos,
		"total":       total,
		"page":        pagination.Page,
		"page_size":   pagination.PageSize,
		"total_pages": totalPages,
		"has_prev":    pagination.Page > 1,
		"has_next":    pagination.Page < totalPages,
		"keyword":     pagination.Keyword,
	})
}

// GetVideoByID 根据ID获取单个视频信息
func GetVideoByID(c *gin.Context) {
	idStr := c.Param("id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, Response{
			Code:    http.StatusBadRequest,
			Message: "ID parameter is required",
		})
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    http.StatusBadRequest,
			Message: "Invalid ID format",
		})
		return
	}

	var video VideoInfo
	result := DB.First(&video, id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, Response{
				Code:    http.StatusNotFound,
				Message: "Video not found",
			})
			return
		}

		log.Printf("Database error when finding video: %v", result.Error)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    http.StatusInternalServerError,
			Message: "Database error",
		})
		return
	}

	c.HTML(http.StatusOK, "edit.tmpl", gin.H{
		"video": video,
		"title": "Edit Video",
	})
}

// UpdateVideo 更新视频信息
func UpdateVideo(c *gin.Context) {
	// 从表单获取数据
	idStr := c.PostForm("id")
	name := c.PostForm("name")
	path := c.PostForm("path")
	size := c.PostForm("size")

	// 验证必填字段
	if idStr == "" || name == "" || path == "" || size == "" {
		c.HTML(http.StatusBadRequest, "edit.tmpl", gin.H{
			"video": VideoInfo{
				Filename: name,
				FilePath: path,
				Filesize: size,
			},
			"error": "All fields are required",
			"title": "Edit Video",
		})
		return
	}

	// 验证ID格式
	videoID, err := strconv.Atoi(idStr)
	if err != nil {
		c.HTML(http.StatusBadRequest, "edit.tmpl", gin.H{
			"video": VideoInfo{
				ID:       videoID,
				Filename: name,
				FilePath: path,
				Filesize: size,
			},
			"error": "Invalid ID format",
			"title": "Edit Video",
		})
		return
	}

	// 查询记录
	var video VideoInfo
	result := DB.First(&video, videoID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			c.HTML(http.StatusNotFound, "edit.tmpl", gin.H{
				"video": VideoInfo{
					ID:       videoID,
					Filename: name,
					FilePath: path,
					Filesize: size,
				},
				"error": "Video record not found",
				"title": "Edit Video",
			})
			return
		}

		log.Printf("Database error when finding video for update: %v", result.Error)
		c.HTML(http.StatusInternalServerError, "edit.tmpl", gin.H{
			"video": VideoInfo{
				ID:       videoID,
				Filename: name,
				FilePath: path,
				Filesize: size,
			},
			"error": "Database error occurred",
			"title": "Edit Video",
		})
		return
	}

	// 更新记录
	video.Filename = name
	video.FilePath = path
	video.Filesize = size
	video.UpdatedAt = time.Now().Format("2006-01-02 15:04:05") // 格式化为字符串

	result = DB.Save(&video)
	if result.Error != nil {
		log.Printf("Database save error: %v", result.Error)
		c.HTML(http.StatusInternalServerError, "edit.tmpl", gin.H{
			"video": video,
			"error": "Update failed: " + result.Error.Error(),
			"title": "Edit Video",
		})
		return
	}

	// 重定向回列表页面
	c.Redirect(http.StatusSeeOther, "/home")
}

// DeleteVideo 删除视频记录
func DeleteVideo(c *gin.Context) {
	idStr := c.Param("id")
	if idStr == "" {
		c.JSON(http.StatusBadRequest, Response{
			Code:    http.StatusBadRequest,
			Message: "ID parameter is required",
		})
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, Response{
			Code:    http.StatusBadRequest,
			Message: "Invalid ID format",
		})
		return
	}

	result := DB.Delete(&VideoInfo{}, id)
	if result.Error != nil {
		log.Printf("Database delete error: %v", result.Error)
		c.JSON(http.StatusInternalServerError, Response{
			Code:    http.StatusInternalServerError,
			Message: "Delete failed",
		})
		return
	}

	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, Response{
			Code:    http.StatusNotFound,
			Message: "Video not found",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: "Video deleted successfully",
	})
}

// HealthCheck 健康检查端点
func HealthCheck(c *gin.Context) {
	if DB.DB().Ping() != nil {
		c.JSON(http.StatusInternalServerError, Response{
			Code:    http.StatusInternalServerError,
			Message: "Database connection failed",
		})
		return
	}

	c.JSON(http.StatusOK, Response{
		Code:    http.StatusOK,
		Message: "Service is healthy",
	})
}

// GetAddPage 显示新增视频页面
func GetAddPage(c *gin.Context) {
	c.HTML(http.StatusOK, "add.tmpl", gin.H{
		"title": "新增视频",
		"video": VideoInfo{}, // 传递一个空对象用于表单绑定
	})
}

// AddVideo 处理新增视频提交
func AddVideo(c *gin.Context) {
	// 从表单获取数据
	name := c.PostForm("name")
	path := c.PostForm("path")
	size := c.PostForm("size")

	// 验证必填字段
	if name == "" || path == "" || size == "" {
		// 如果有错误，重新渲染表单并显示错误信息，保留用户输入
		c.HTML(http.StatusBadRequest, "add.tmpl", gin.H{
			"title": "新增视频",
			"video": VideoInfo{
				Filename: name,
				FilePath: path,
				Filesize: size,
			},
			"error": "所有字段均为必填项",
		})
		return
	}

	// 创建新记录
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	newVideo := VideoInfo{
		Filename:  name,
		FilePath:  path,
		Filesize:  size,
		CreatedAt: nowStr,
		UpdatedAt: nowStr,
	}

	// 保存到数据库
	result := DB.Create(&newVideo)
	if result.Error != nil {
		log.Printf("Database create error: %v", result.Error)
		c.HTML(http.StatusInternalServerError, "add.tmpl", gin.H{
			"title": "新增视频",
			"video": newVideo,
			"error": "保存失败: " + result.Error.Error(),
		})
		return
	}

	// 成功则重定向回列表页
	c.Redirect(http.StatusSeeOther, "/home")
}

// 全局数据库变量
var DB *gorm.DB

func main() {
	// 初始化数据库
	DB = initDB()
	defer DB.Close()

	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)

	// 创建路由器
	router := gin.Default()

	// 中间件
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	// 静态文件服务
	router.Static("/static", "./static")

	// 加载模板
	router.HTMLRender = loadTemplates("./template")

	// 路由定义
	router.GET("/health", HealthCheck)
	router.GET("/home", GetVideos)
	router.GET("/add", GetAddPage)
	router.POST("/add", AddVideo)
	router.GET("/edit/:id", GetVideoByID)
	router.POST("/update", UpdateVideo)
	router.DELETE("/delete/:id", DeleteVideo)

	// 启动服务器
	log.Println("Server starting on :8080")
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
