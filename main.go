package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"github.com/SheltonZhu/115driver/pkg/driver"
)

const DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/5.37.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

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

// 播放流信息
type StreamInfo struct {
	Quality string `json:"quality"`
	URL     string `json:"url"`
	IsM3U8  bool   `json:"is_m3u8"`
}

// 多播放流响应
type StreamsResponse struct {
	Success       bool         `json:"success"`
	Error         string       `json:"error,omitempty"`
	Streams       []StreamInfo `json:"streams"`
	UserAgent     string       `json:"user_agent"`
	DirPath       string       `json:"dir_path,omitempty"`
	FilenameNoExt string       `json:"filename_no_ext,omitempty"`
}

func main() {
	var (
		action = flag.String("action", "", "操作类型: list, play, get-streams")
		path   = flag.String("path", "", "路径")
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
		err = handleList(client, *path)
	case "play":
		if *path == "" {
			outputError("play操作需要提供--path参数")
			return
		}
		err = handlePlay(client, *path)
	case "get-streams":
		if *path == "" {
			outputError("get-streams操作需要提供--path参数")
			return
		}
		err = handleGetStreams(client, *path)
	default:
		outputError("未知操作: " + *action)
	}

	if err != nil {
		outputError(err.Error())
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

func handleList(client *driver.Pan115Client, path string) error {
	// 使用更高效的DirName2CID方法解析路径
	var cid string
	if path == "/" || path == "" {
		cid = "0"
	} else {
		result, err := client.DirName2CID(path)
		if err != nil {
			return fmt.Errorf("解析路径失败: %w", err)
		}
		cid = string(result.CategoryID)
	}

	// 获取文件列表 - 按名称排序
	files, err := getFilesSortedByName(client, cid, 1000)
	if err != nil {
		return fmt.Errorf("获取文件列表失败: %w", err)
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
	return nil
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

func handlePlay(client *driver.Pan115Client, filePath string) error {
	targetFile, _, err := resolveFilePickCode(client, filePath)
	if err != nil {
		return err
	}

	// 获取下载链接
	downloadInfo, err := client.DownloadWithUA(targetFile.PickCode, DefaultUserAgent)
	if err != nil {
		return fmt.Errorf("获取下载链接失败: %w", err)
	}

	// 构建响应
	response := map[string]interface{}{
		"success": true,
		"url":     downloadInfo.Url.Url,
	}

	outputJSON(response)
	return nil
}

func handleGetStreams(client *driver.Pan115Client, filePath string) error {
	targetFile, _, err := resolveFilePickCode(client, filePath)
	if err != nil {
		return err
	}
	pickCode := targetFile.PickCode

	streams := make([]StreamInfo, 0)
	userAgent := DefaultUserAgent

	// 1. 获取原始文件URL
	downloadInfo, err := client.DownloadWithUA(pickCode, userAgent)
	if err == nil && downloadInfo.Url.Url != "" {
		streams = append(streams, StreamInfo{
			Quality: "Source",
			URL:     downloadInfo.Url.Url,
			IsM3U8:  false,
		})
	}

	// 2. 获取M3U8流
	m3u8Url := fmt.Sprintf("https://115.com/api/video/m3u8/%s.m3u8", pickCode)
	req := client.NewRequest().SetHeader("User-Agent", userAgent)
	resp, err := req.Get(m3u8Url)
	if err == nil {
		body := resp.String()
		if strings.HasPrefix(body, "#EXTM3U") {
			lines := strings.Split(body, "\n")
			for i, line := range lines {
				if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
					if i+1 < len(lines) {
						streamURL := strings.TrimSpace(lines[i+1])
						if strings.HasPrefix(streamURL, "http") {
							quality := "Unknown"
							re := regexp.MustCompile(`RESOLUTION=\d+x(\d+)`)
							matches := re.FindStringSubmatch(line)
							if len(matches) > 1 {
								quality = matches[1] + "p"
							}
							streams = append(streams, StreamInfo{
								Quality: quality,
								URL:     streamURL,
								IsM3U8:  true,
							})
						}
					}
				}
			}
		}
	}

	if len(streams) == 0 {
		return fmt.Errorf("未能获取任何播放链接")
	}

	// 3. 排序
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].Quality == "Source" {
			return true
		}
		if streams[j].Quality == "Source" {
			return false
		}
		q_i, err_i := strconv.Atoi(strings.TrimSuffix(streams[i].Quality, "p"))
		q_j, err_j := strconv.Atoi(strings.TrimSuffix(streams[j].Quality, "p"))
		if err_i != nil || err_j != nil {
			return false // or some other default behavior
		}
		return q_i > q_j
	})

	// 提取文件名（不带扩展名）
	fileName := filepath.Base(filePath)
	filenameNoExt := fileName
	if lastDot := strings.LastIndex(fileName, "."); lastDot != -1 {
		filenameNoExt = fileName[:lastDot]
	}

	response := StreamsResponse{
		Success:       true,
		Streams:       streams,
		UserAgent:     userAgent,
		DirPath:       filepath.Dir(filePath),
		FilenameNoExt: filenameNoExt,
	}
	outputJSON(response)
	return nil
}

// 提取 resolveFilePickCode 作为一个可复用的辅助函数
func resolveFilePickCode(client *driver.Pan115Client, filePath string) (*driver.File, string, error) {
	// 分离目录和文件名
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		return nil, "", fmt.Errorf("无效的文件路径")
	}

	dirPath := filePath[:lastSlash]
	fileName := filePath[lastSlash+1:]

	if dirPath == "" {
		dirPath = "/"
	}

	var dirCid string
	if dirPath == "/" {
		dirCid = "0"
	} else {
		result, err := client.DirName2CID(dirPath)
		if err != nil {
			return nil, "", fmt.Errorf("解析目录路径失败: %w", err)
		}
		dirCid = string(result.CategoryID)
	}

	files, err := getFilesSortedByName(client, dirCid, 1000)
	if err != nil {
		return nil, "", fmt.Errorf("获取目录内容失败: %w", err)
	}

	var targetFile *driver.File
	for i := range *files {
		file := (*files)[i]
		if !file.IsDirectory && file.Name == fileName {
			targetFile = &file
			break
		}
	}

	if targetFile == nil {
		return nil, "", fmt.Errorf("文件不存在: %s", fileName)
	}

	return targetFile, targetFile.PickCode, nil
}


// getFilesSortedByName 按名称排序获取文件列表
func getFilesSortedByName(client *driver.Pan115Client, dirID string, limit int64) (*[]driver.File, error) {
	if dirID == "" {
		dirID = "0"
	}

	// 使用115的natsort API按名称排序
	req := client.NewRequest().ForceContentType("application/json;charset=UTF-8")
	params := map[string]string{
		"aid":      "1",
		"cid":      dirID,
		"o":        driver.FileOrderByName,
		"asc":      "1",
		"offset":   "0",
		"show_dir": "1",
		"limit":    fmt.Sprintf("%d", limit),
		"snap":     "0",
		"natsort":  "1", // 启用自然排序
		"record_open_time": "1",
		"format":   "json",
		"fc_mix":   "0",
	}

	var result driver.FileListResp
	req = req.SetQueryParams(params).SetResult(&result)

	resp, err := req.Get(driver.ApiFileListByName)
	if err = driver.CheckErr(err, &result, resp); err != nil {
		return nil, err
	}

	var files []driver.File
	for _, fileInfo := range result.Files {
		files = append(files, *(&driver.File{}).From(&fileInfo))
	}

	return &files, nil
}
