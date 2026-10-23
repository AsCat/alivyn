package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AsCat/alivyn/internal/config"
	"github.com/AsCat/alivyn/internal/display"
	"github.com/AsCat/alivyn/internal/model"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
	"github.com/AsCat/alivyn/internal/tui"
)

func initialize(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	fmt.Fprintln(out, "Alivyn is AI; its personality is simulated. / 意续是 AI；人格体验由程序建模。")
	language, err := prompt(reader, out, "Language / 语言 (en/zh-CN)", "en")
	if err != nil {
		return err
	}
	if language != "en" && language != "zh-CN" {
		return errors.New("language must be en or zh-CN / 语言必须为 en 或 zh-CN")
	}
	label := func(en, zh string) string {
		if language == "zh-CN" {
			return zh
		}
		return en
	}
	name, err := prompt(reader, out, label("Individual name", "个体的名字"), "Ari")
	if err != nil {
		return err
	}
	provider, err := prompt(reader, out, label("Model provider (deepseek/custom)", "模型提供方 (deepseek/custom)"), "deepseek")
	if err != nil {
		return err
	}
	baseURL := config.DefaultDeepSeekURL
	modelName := config.DefaultDeepSeekModel
	if provider == "custom" {
		baseURL, err = prompt(reader, out, label("OpenAI-compatible base URL", "兼容接口地址"), "")
		if err != nil {
			return err
		}
		modelName, err = prompt(reader, out, label("Model ID", "模型 ID"), "")
		if err != nil {
			return err
		}
	}
	c := config.Config{SchemaVersion: 1, Name: name, Language: language, Provider: provider, BaseURL: baseURL, Model: modelName}
	if err := c.Validate(); err != nil {
		return err
	}
	if err := config.SaveNew(c); err != nil {
		return err
	}
	dir, err := config.Directory()
	if err != nil {
		return err
	}
	history, err := store.OpenHistory(context.Background(), dir)
	if err != nil {
		return err
	}
	if err := history.Close(); err != nil {
		return err
	}
	fmt.Fprintf(out, label("Created %s. Set ALIVYN_API_KEY, then run 'alivyn model check' and 'alivyn'.\n", "已创建 %s。设置 ALIVYN_API_KEY 后，运行 'alivyn model check' 和 'alivyn'。\n"), c.Name)
	return nil
}

func prompt(reader *bufio.Reader, out io.Writer, label, fallback string) (string, error) {
	if fallback == "" {
		fmt.Fprintf(out, "%s: ", label)
	} else {
		fmt.Fprintf(out, "%s [%s]: ", label, fallback)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		value = fallback
	}
	return value, nil
}

func openConfiguredHistory(ctx context.Context) (config.Config, *store.History, error) {
	c, err := config.Load()
	if err != nil {
		return config.Config{}, nil, err
	}
	dir, err := config.Directory()
	if err != nil {
		return config.Config{}, nil, err
	}
	h, err := store.OpenHistory(ctx, dir)
	return c, h, err
}

func configuredClient(c config.Config) (model.Client, error) {
	key := os.Getenv("ALIVYN_API_KEY")
	if key == "" {
		return model.Client{}, errors.New("ALIVYN_API_KEY is not set")
	}
	return model.Client{BaseURL: c.BaseURL, APIKey: key}, nil
}

func checkModel(ctx context.Context, out io.Writer) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	_, err = client.Complete(ctx, c.Model, []model.Message{{Role: "user", Content: "Reply with OK."}})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Model connection: available")
	return nil
}

func chat(ctx context.Context, in io.Reader, out io.Writer) error {
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	s := session.Session{Config: c, History: h, Client: client}
	if c.Language == "zh-CN" {
		fmt.Fprintf(out, "%s 是 AI。输入 /exit 退出；对话会保存在这台设备。\n", c.Name)
	} else {
		fmt.Fprintf(out, "%s is AI. Type /exit to leave; your conversation will remain on this device.\n", c.Name)
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for {
		fmt.Fprint(out, "you> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "/exit" {
			return nil
		}
		if input == "" {
			continue
		}
		if err := s.AddUser(ctx, input); err != nil {
			return err
		}
		reply, err := s.GenerateReply(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s> %s\n", c.Name, display.Clean(reply))
	}
}

func runTUI() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	client, err := configuredClient(c)
	if err != nil {
		return err
	}
	return tui.Run(ctx, session.Session{Config: c, History: h, Client: client})
}

func showHistory(ctx context.Context, out io.Writer) error {
	_, h, err := openConfiguredHistory(ctx)
	if err != nil {
		return err
	}
	defer h.Close()
	entries, err := h.Recent(ctx, 100)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fmt.Fprintf(out, "%s %s: %s\n", entry.CreatedAt.Local().Format("2006-01-02 15:04"), entry.Role, display.Clean(entry.Content))
	}
	return nil
}
