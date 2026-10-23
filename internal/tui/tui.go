package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/AsCat/alivyn/internal/display"
	"github.com/AsCat/alivyn/internal/session"
	"github.com/AsCat/alivyn/internal/store"
)

const visibleHistory = 50

type replyMsg struct {
	err error
}

type conversation struct {
	ctx      context.Context
	session  session.Session
	entries  []store.Entry
	input    textarea.Model
	view     viewport.Model
	width    int
	height   int
	pending  bool
	status   string
	hasError bool
}

func Run(ctx context.Context, s session.Session) error {
	entries, err := s.History.Recent(ctx, visibleHistory)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(newConversation(ctx, s, entries)).Run()
	return err
}

func newConversation(ctx context.Context, s session.Session, entries []store.Entry) conversation {
	input := textarea.New()
	input.Prompt = "┃ "
	input.Placeholder = choose(s.Config.Language, "Say something...", "想说些什么……")
	input.ShowLineNumbers = false
	input.CharLimit = 10000
	input.SetHeight(3)
	input.SetWidth(76)
	input.SetVirtualCursor(false)
	input.KeyMap.InsertNewline.SetEnabled(false)
	input.Focus()
	styles := input.Styles()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	input.SetStyles(styles)
	m := conversation{
		ctx: ctx, session: s, entries: entries, input: input,
		view:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(17)),
		width: 80, height: 24,
		status: choose(s.Config.Language, "Ready", "随时可以说话"),
	}
	m.renderHistory()
	return m
}

func (m conversation) Init() tea.Cmd { return textarea.Blink }

func (m conversation) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.input.Value() == "" {
				return m, tea.Quit
			}
			m.input.Reset()
			return m, nil
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.view, cmd = m.view.Update(msg)
			return m, cmd
		case "enter":
			if m.pending {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			if text == "/exit" || text == "/quit" {
				return m, tea.Quit
			}
			if err := m.session.AddUser(m.ctx, text); err != nil {
				m.showError(err)
				return m, nil
			}
			if err := m.reload(); err != nil {
				m.showError(err)
				return m, nil
			}
			m.input.Reset()
			m.pending = true
			m.hasError = false
			m.status = choose(m.session.Config.Language, "Thinking…", "正在整理回应……")
			return m, func() tea.Msg {
				_, err := m.session.GenerateReply(m.ctx)
				return replyMsg{err: err}
			}
		}
	case replyMsg:
		m.pending = false
		if msg.err != nil {
			m.showError(msg.err)
			return m, nil
		}
		if err := m.reload(); err != nil {
			m.showError(err)
			return m, nil
		}
		m.hasError = false
		m.status = choose(m.session.Config.Language, "Ready", "随时可以说话")
		return m, nil
	case cursor.BlinkMsg:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(message)
	return m, cmd
}

func (m *conversation) resize(width, height int) {
	if width < 20 {
		width = 20
	}
	if height < 7 {
		height = 7
	}
	m.width, m.height = width, height
	inputHeight := 3
	if height < 12 {
		inputHeight = 1
	}
	m.input.SetWidth(width - 2)
	m.input.SetHeight(inputHeight)
	m.view.SetWidth(width)
	m.view.SetHeight(height - 4 - inputHeight)
	m.renderHistory()
}

func (m *conversation) reload() error {
	entries, err := m.session.History.Recent(m.ctx, visibleHistory)
	if err != nil {
		return err
	}
	m.entries = entries
	m.renderHistory()
	return nil
}

func (m *conversation) renderHistory() {
	blocks := make([]string, 0, len(m.entries))
	for _, entry := range m.entries {
		label := choose(m.session.Config.Language, "You", "你")
		color := "#B6C2D9"
		if entry.Role == "assistant" {
			label = m.session.Config.Name
			color = "#86D3D3"
		}
		text := display.Clean(entry.Content)
		blocks = append(blocks, lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(label)+"  "+text)
	}
	if len(blocks) == 0 {
		blocks = append(blocks, choose(m.session.Config.Language,
			"This is one continuous conversation. Your shared history will appear here.",
			"这里是一段持续的对话。你们共同经历的消息会留在这里。"))
	}
	content := lipgloss.NewStyle().Width(m.view.Width()).Render(strings.Join(blocks, "\n\n"))
	m.view.SetContent(content)
	m.view.GotoBottom()
}

func (m *conversation) showError(err error) {
	m.hasError = true
	m.status = choose(m.session.Config.Language, "Error: ", "出错了：") + display.Clean(err.Error())
}

func (m conversation) View() tea.View {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#86D3D3")).Render(m.session.Config.Name + "  ·  Alivyn")
	subtitle := choose(m.session.Config.Language, "An AI individual · one continuous conversation", "AI 个体 · 一段持续的对话")
	status := m.status
	if m.hasError {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("#E8A09A")).Render(status)
	}
	help := choose(m.session.Config.Language, "Enter send  ·  PgUp/PgDn history  ·  Esc quit", "Enter 发送  ·  PgUp/PgDn 查看历史  ·  Esc 退出")
	content := title + "\n" + subtitle + "\n" + m.view.View() + "\n" + status + "\n" + m.input.View() + "\n" + help
	view := tea.NewView(content)
	if cursor := m.input.Cursor(); cursor != nil {
		cursor.Y += 2 + m.view.Height() + 1
		view.Cursor = cursor
	}
	view.AltScreen = true
	return view
}

func choose(language, en, zh string) string {
	if language == "zh-CN" {
		return zh
	}
	return en
}
