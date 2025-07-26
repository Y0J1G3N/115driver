package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SheltonZhu/115driver/pkg/driver"
)

// 文件列表响应
type ListResponse struct {
	Success bool       `json:"success"`
	Error   string     `json:"error,omitempty"`
	Items   []FileItem `json:"items"`
}

// 文件项
type FileItem struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Extension string `json:"extension,omitempty"`
	NameNoExt string `json:"name_no_ext,omitempty"`
}

// 播放响应
type PlayResponse struct {
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	URL           string `json:"url"`
	UserAgent     string `json:"user_agent"`
	DirPath       string `json:"dir_path,omitempty"`
	FilenameNoExt string `json:"filename_no_ext,omitempty"`
}

func main() {
	var (
		action = flag.String("action", "", "操作类型: list, play")
		path   = flag.String("path", "", "路径")
		sort   = flag.String("sort", "", "排序方式: name, time, size, type (默认按时间)")
	)
	flag.Parse()

	if *action == "" {
		outputError("必须指定action参数")
		return
	}

	// 确定cookies文件路径 - 相对于程序文件位置
	execPath, _ := os.Executable()
	cookiesFile := filepath.Join(filepath.Dir(filepath.Dir(execPath)), "data", "115")

	// 初始化115客户端
	client, err := initClient(cookiesFile)
	if err != nil {
		outputError("初始化客户端失败: " + err.Error())
		return
	}

	// 执行操作
	switch *action {
	case "list":
		if *path == "" {
			*path = "/"
		}
		handleList(client, *path, *sort)
	case "play":
		if *path == "" {
			outputError("play操作需要提供--path参数")
			return
		}
		handlePlay(client, *path)
	default:
		outputError("未知操作: " + *action)
	}
}



func initClient(cookiesFile string) (*driver.Pan115Client, error) {
	// 检查cookies文件是否存在
	if _, err := os.Stat(cookiesFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("cookies文件不存在: %s", cookiesFile)
	}

	// 读取cookies文件
	cookieData, err := os.ReadFile(cookiesFile)
	if err != nil {
		return nil, fmt.Errorf("读取cookies文件失败: %v", err)
	}

	// 解析cookies
	cr := &driver.Credential{}
	if err := cr.FromCookie(strings.TrimSpace(string(cookieData))); err != nil {
		return nil, fmt.Errorf("解析cookies失败: %v", err)
	}

	// 创建客户端
	client := driver.Defalut().ImportCredential(cr)
	
	// 检查登录状态
	if err := client.LoginCheck(); err != nil {
		return nil, fmt.Errorf("登录检查失败: %v", err)
	}

	return client, nil
}

func handleList(client *driver.Pan115Client, path string, sortBy string) {
	// 解析路径为CID
	cid, err := resolvePath(client, path)
	if err != nil {
		outputError("解析路径失败: " + err.Error())
		return
	}

	// 获取文件列表 - 支持自定义排序
	files, err := getFilesSorted(client, cid, 1000, sortBy)
	if err != nil {
		outputError("获取文件列表失败: " + err.Error())
		return
	}

	// 转换为CLI格式
	items := make([]FileItem, 0, len(*files))
	for _, file := range *files {
		item := FileItem{
			Name: file.Name,
			Type: "file",
		}

		if file.IsDirectory {
			item.Type = "dir"
			item.Name += "/"
		} else {
			// 提取文件扩展名和不带扩展名的文件名
			if lastDot := strings.LastIndex(file.Name, "."); lastDot != -1 {
				item.Extension = strings.ToLower(file.Name[lastDot+1:])
				item.NameNoExt = file.Name[:lastDot]
			} else {
				item.NameNoExt = file.Name
			}
		}

		items = append(items, item)
	}

	// 构建响应
	response := ListResponse{
		Success: true,
		Items:   items,
	}

	outputJSON(response)
}



func outputError(message string) {
	response := map[string]interface{}{
		"success": false,
		"error":   message,
	}
	outputJSON(response)
	os.Exit(1)
}

func outputJSON(data interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	encoder.Encode(data)
}



// 解析友好路径为CID
func resolvePath(client *driver.Pan115Client, friendlyPath string) (string, error) {
	// 移除开头的斜杠
	if strings.HasPrefix(friendlyPath, "/") {
		friendlyPath = friendlyPath[1:]
	}

	// 如果是空路径，返回根目录
	if friendlyPath == "" {
		return "0", nil
	}

	// 分割路径
	parts := strings.Split(friendlyPath, "/")
	currentCid := "0"

	// 逐级查找
	for _, part := range parts {
		if part == "" {
			continue
		}

		// 获取当前目录的内容
		files, err := client.List(currentCid)
		if err != nil {
			return "", fmt.Errorf("获取目录内容失败: %v", err)
		}

		// 查找匹配的子目录
		found := false
		for _, file := range *files {
			if file.IsDirectory && file.Name == part {
				currentCid = file.FileID
				found = true
				break
			}
		}

		if !found {
			return "", fmt.Errorf("路径不存在: %s", part)
		}
	}

	return currentCid, nil
}

func handlePlay(client *driver.Pan115Client, filePath string) {
	// 解析文件路径，找到文件的pickcode

	// 分离目录和文件名
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		outputError("无效的文件路径")
		return
	}

	dirPath := filePath[:lastSlash]
	fileName := filePath[lastSlash+1:]

	if dirPath == "" {
		dirPath = "/"
	}

	// 获取目录内容，查找文件
	var dirCid string
	if dirPath == "/" {
		dirCid = "0"
	} else {
		resolvedCid, err := resolvePath(client, dirPath)
		if err != nil {
			outputError("解析目录路径失败: " + err.Error())
			return
		}
		dirCid = resolvedCid
	}

	// 获取目录中的文件列表
	files, err := client.ListWithLimit(dirCid, 10000)
	if err != nil {
		outputError("获取目录内容失败: " + err.Error())
		return
	}

	// 查找指定文件
	var targetFile *driver.File
	for _, file := range *files {
		if !file.IsDirectory && file.Name == fileName {
			targetFile = &file
			break
		}
	}

	if targetFile == nil {
		outputError("文件不存在: " + fileName)
		return
	}

	// 获取下载链接
	userAgent := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"
	downloadInfo, err := client.DownloadWithUA(targetFile.PickCode, userAgent)
	if err != nil {
		outputError("获取下载链接失败: " + err.Error())
		return
	}

	// 提取文件名（不带扩展名）
	filenameNoExt := fileName
	if lastDot := strings.LastIndex(fileName, "."); lastDot != -1 {
		filenameNoExt = fileName[:lastDot]
	}

	// 构建响应
	response := PlayResponse{
		Success:       true,
		URL:           downloadInfo.Url.Url,
		UserAgent:     userAgent,
		DirPath:       dirPath,
		FilenameNoExt: filenameNoExt,
	}

	outputJSON(response)
}

// getSortOrder 根据排序参数返回对应的排序常量
func getSortOrder(sortBy string) string {
	switch sortBy {
	case "name":
		return driver.FileOrderByName
	case "time":
		return driver.FileOrderByTime
	case "size":
		return driver.FileOrderBySize
	case "type":
		return driver.FileOrderByType
	default:
		return driver.FileOrderByTime // 默认按时间排序
	}
}

// 注意：driver.WithAsc 函数有bug，但 DefaultGetFileOptions 默认就是升序 (asc: "1")
// 所以我们不需要额外设置 asc 字段

// getFilesSorted 获取排序后的文件列表
func getFilesSorted(client *driver.Pan115Client, dirID string, limit int64, sortBy string) (*[]driver.File, error) {
	// 如果没有指定排序，使用默认的 ListWithLimit
	if sortBy == "" {
		return client.ListWithLimit(dirID, limit)
	}

	// 使用自定义排序
	req := client.NewRequest().ForceContentType("application/json;charset=UTF-8")
	getFilesOpts := []driver.GetFileOptions{
		driver.WithApiURL(driver.ApiFileList),
		driver.WithLimit(limit),
		driver.WithOffset(0),
		driver.WithOrder(getSortOrder(sortBy)),
		// 默认就是升序，不需要额外设置
	}

	result, err := driver.GetFiles(req, dirID, getFilesOpts...)
	if err != nil {
		return nil, err
	}

	var files []driver.File
	for _, fileInfo := range result.Files {
		files = append(files, *(&driver.File{}).From(&fileInfo))
	}

	return &files, nil
}
