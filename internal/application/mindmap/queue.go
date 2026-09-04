package mindmap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	domain "github.com/nzlov/anycode/internal/domain/mindmap"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
)

const asyncTaskPromptGuidance = "你正在执行异步项目思维图整理任务。只处理当前会话产生的稳定需求、功能、决策和关系。定位已有概念时调用 `mind_map_search`。query 使用 SQL 风格布尔表达式，查询词通过 AND/OR 组合，AND 优先于 OR，可用括号改变优先级，相邻查询词之间必须有操作符，例如 `workflow AND (runner OR retry)`。limit 通常取 3–5，不要读取全图、批量组合多次搜索，或把搜索与高输出命令放进同一次工具调用。搜索结果中的命中节点含完整信息和 Tag，关联节点仅为摘要，并返回可直接传给 `mind_map_update` 的 tagRevision。对每组将变更的概念只做必要的窄搜索，优先复用已有节点；整理好操作后尽量一次批量更新。把最近一次搜索的 tagRevision 原样传给 `mind_map_update`；仅在没有新鲜搜索结果或更新提示 revision 过期时调用 `mind_map_tags` 刷新。`mind_map_tags` 返回 JSON 文本，在同一次代码工具调用中读取字段前必须先解析。Agent 不得创建、更新、删除 Tag 节点或直接维护 Tag 关系；每个节点 upsert 必须携带完整的 Tag 名称列表，后端负责规范化 Tag、生成受控 ID、对账关系并清理孤儿 Tag，且只有 Tag 可以作为 `project-root` 的一级节点。节点标题不能为空，一个节点只表达一个稳定概念。引用实现代码的节点必须携带非空 files，逐项记录准确的文件、方法和起止行号；非代码节点不得携带文件位置，文件列表不得写入节点内容。唯一固定节点是标题为项目名、ID 为 `project-root` 的中心根节点，不得修改、删除或移动。禁止创建仅记录错误、失败、临时调试、交付状态、提交或测试结果的节点；只清理当前会话创建或修改后已失效的此类节点及其关系，不得为全库清理执行宽泛搜索。没有稳定图变化时不要调用更新工具。完成后直接结束。"

type Queue struct {
	repo      domain.Repository
	uow       port.UnitOfWork
	sessions  sessiondomain.Repository
	projects  projectdomain.Repository
	settings  settingdomain.MindMapConfigurationProvider
	processes processdomain.Repository
	codex     processdomain.CodexProcess
	now       func() time.Time
	newRunID  func() (processdomain.RunID, error)
	wake      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
}

func NewQueue(repo domain.Repository, uow port.UnitOfWork, sessions sessiondomain.Repository, projects projectdomain.Repository, settings settingdomain.MindMapConfigurationProvider, processes processdomain.Repository, codex processdomain.CodexProcess) *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	return &Queue{
		repo: repo, uow: uow, sessions: sessions, projects: projects, settings: settings, processes: processes, codex: codex,
		now: time.Now, newRunID: generateProcessRunID, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel,
	}
}

func (q *Queue) Start() {
	if q == nil {
		return
	}
	q.startOnce.Do(func() {
		if err := q.recoverRunning(q.ctx); err != nil {
			log.Printf("recover mind map queue: %v", err)
		}
		q.wg.Add(1)
		go q.loop()
		q.Schedule()
	})
}

func (q *Queue) Close() {
	if q == nil || q.cancel == nil {
		return
	}
	var tasks []domain.Task
	var err error
	if q.repo != nil {
		tasks, err = q.repo.ListTasks(context.Background(), "")
	}
	q.cancel()
	if err == nil && q.codex != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, task := range tasks {
			if task.Status == domain.TaskRunning && task.ProcessRunID != "" {
				_ = q.codex.Stop(stopCtx, processdomain.RunID(task.ProcessRunID))
			}
		}
	}
	q.wg.Wait()
}

func (q *Queue) Schedule() {
	if q == nil || q.wake == nil {
		return
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) loop() {
	defer q.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-q.ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
		if err := q.drain(q.ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("drain mind map queue: %v", err)
		}
	}
}

func (q *Queue) drain(ctx context.Context) error {
	if q.repo == nil || q.settings == nil || q.processes == nil || q.codex == nil {
		return nil
	}
	configuration, err := q.settings.MindMapConfiguration(ctx)
	if err != nil {
		return err
	}
	if !configuration.Enabled || configuration.Mode != settingdomain.MindMapModeAsync {
		return nil
	}
	running, err := q.repo.CountRunningTasks(ctx)
	if err != nil {
		return err
	}
	available := configuration.MaxConcurrent - running
	if available <= 0 {
		return nil
	}
	tasks, err := q.repo.ListQueuedTasks(ctx, available)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		claimed, err := q.claim(ctx, task)
		if err != nil {
			return err
		}
		if claimed == nil {
			continue
		}
		q.wg.Add(1)
		go func(task domain.Task, snapshot settingdomain.MindMapConfiguration) {
			defer q.wg.Done()
			q.run(task, snapshot)
		}(*claimed, configuration)
	}
	return nil
}

func (q *Queue) claim(ctx context.Context, expected domain.Task) (*domain.Task, error) {
	runID, err := q.newRunID()
	if err != nil {
		return nil, err
	}
	now := q.now()
	var claimed *domain.Task
	claim := func(ctx context.Context, maps domain.Repository, processes processdomain.Repository) error {
		current, err := maps.FindTask(ctx, expected.ID)
		if err != nil {
			return err
		}
		if current.Status != domain.TaskQueued {
			return nil
		}
		current.Status = domain.TaskRunning
		current.ProcessRunID = string(runID)
		current.Attempts++
		current.Error = ""
		current.StartedAt = &now
		current.FinishedAt = nil
		current.UpdatedAt = now
		if err := processes.CreateRun(ctx, processdomain.Run{
			ID: runID, SessionID: processdomain.SessionID(current.SessionID), Status: processdomain.StatusStarting, StartedAt: now,
		}); err != nil {
			return err
		}
		if err := maps.SaveTask(ctx, current); err != nil {
			return err
		}
		claimed = &current
		return nil
	}
	if q.uow != nil {
		if err := q.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error { return claim(ctx, tx.MindMaps(), tx.Processes()) }); err != nil {
			return nil, err
		}
	} else if err := claim(ctx, q.repo, q.processes); err != nil {
		return nil, err
	}
	return claimed, nil
}

func (q *Queue) run(task domain.Task, configuration settingdomain.MindMapConfiguration) {
	ctx := q.ctx
	session, err := q.sessions.Find(ctx, sessiondomain.ID(task.SessionID))
	if err != nil {
		q.fail(task, fmt.Errorf("find session: %w", err))
		return
	}
	project, err := q.projects.Find(ctx, projectdomain.ID(task.ProjectID))
	if err != nil {
		q.fail(task, fmt.Errorf("find project: %w", err))
		return
	}
	input := processdomain.CodexStartInput{
		ProcessRunID: processdomain.RunID(task.ProcessRunID), SessionID: processdomain.SessionID(task.SessionID),
		Workdir: project.Path.Value, Input: []processdomain.CodexInputItem{{Type: "text", Text: asyncTaskPrompt(session)}},
		Action: processdomain.CodexActionTurn, DeveloperInstructions: asyncTaskPromptGuidance,
		Model: configuration.Model, ReasoningEffort: configuration.ReasoningEffort, PermissionMode: "read-only",
		DynamicTools: []processdomain.DynamicToolName{processdomain.DynamicToolMindMapSearch, processdomain.DynamicToolMindMapTags, processdomain.DynamicToolMindMapUpdate},
	}
	var handle processdomain.CodexHandle
	if strings.TrimSpace(session.CodexSessionID) != "" {
		handle, err = q.codex.Resume(ctx, processdomain.CodexResumeInput{
			ProcessRunID: input.ProcessRunID, SessionID: input.SessionID, CodexSessionID: session.CodexSessionID,
			Workdir: input.Workdir, Input: input.Input, Action: input.Action, DeveloperInstructions: input.DeveloperInstructions,
			Model: input.Model, ReasoningEffort: input.ReasoningEffort, PermissionMode: input.PermissionMode, DynamicTools: input.DynamicTools,
		})
		if err != nil {
			handle, err = q.codex.Start(ctx, input)
		}
	} else {
		handle, err = q.codex.Start(ctx, input)
	}
	if err != nil {
		q.fail(task, fmt.Errorf("start mind map analysis: %w", err))
		return
	}
	if err := q.processes.MarkRunning(ctx, processdomain.RunID(task.ProcessRunID), handle.CodexSessionID); err != nil {
		_ = q.codex.Stop(context.Background(), processdomain.RunID(task.ProcessRunID))
		q.fail(task, fmt.Errorf("mark mind map process running: %w", err))
		return
	}
	events, err := q.codex.Events(ctx, handle)
	if err != nil {
		q.fail(task, fmt.Errorf("consume mind map events: %w", err))
		return
	}
	var result processdomain.ExitResult
	sawExit := false
	for event := range events {
		if event.Type != processdomain.CodexEventProcessExit {
			continue
		}
		if current, ok := event.Content.(processdomain.ExitResult); ok {
			result = current
			sawExit = true
		}
	}
	if !sawExit {
		q.fail(task, errors.New("mind map process ended without an exit event"))
		return
	}
	if result.FailureReason != "" || result.ExitCode != nil && *result.ExitCode != 0 {
		q.failWithResult(task, result)
		return
	}
	q.complete(task, result)
}

func (q *Queue) complete(task domain.Task, result processdomain.ExitResult) {
	now := q.now()
	if result.FinishedAt.IsZero() {
		result.FinishedAt = now
	}
	err := q.uow.Do(context.Background(), func(ctx context.Context, tx port.Tx) error {
		current, err := tx.MindMaps().FindTask(ctx, task.ID)
		if err != nil {
			return err
		}
		if current.Status != domain.TaskRunning || current.ProcessRunID != task.ProcessRunID {
			return nil
		}
		project, err := tx.Projects().Find(ctx, projectdomain.ID(task.ProjectID))
		if err != nil {
			return err
		}
		graph, _, err := tx.MindMaps().FindGraph(ctx, task.ProjectID)
		if err != nil {
			return err
		}
		graph.ProjectID = task.ProjectID
		domain.EnsureRoot(&graph, project.Name, project.UpdatedAt)
		overlay, found, err := tx.MindMaps().FindOverlay(ctx, task.SessionID)
		if err != nil {
			return err
		}
		if found {
			domain.MergeOverlay(&graph, overlay)
			domain.Touch(&graph, now)
			if err := tx.MindMaps().SaveGraph(ctx, graph, overlay.Changes); err != nil {
				return err
			}
			if err := tx.MindMaps().DeleteOverlay(ctx, task.SessionID); err != nil {
				return err
			}
		}
		current.Status = domain.TaskCompleted
		current.Error = ""
		current.FinishedAt = &now
		current.UpdatedAt = now
		if err := tx.MindMaps().SaveTask(ctx, current); err != nil {
			return err
		}
		return tx.Processes().MarkExited(ctx, processdomain.RunID(task.ProcessRunID), result)
	})
	if err != nil {
		q.fail(task, fmt.Errorf("complete mind map task: %w", err))
		return
	}
	q.Schedule()
}

func (q *Queue) fail(task domain.Task, cause error) {
	q.failWithResult(task, processdomain.ExitResult{FailureReason: cause.Error(), FinishedAt: q.now()})
}

func (q *Queue) failWithResult(task domain.Task, result processdomain.ExitResult) {
	if q.ctx.Err() != nil {
		return
	}
	now := q.now()
	if result.FinishedAt.IsZero() {
		result.FinishedAt = now
	}
	err := q.uow.Do(context.Background(), func(ctx context.Context, tx port.Tx) error {
		current, err := tx.MindMaps().FindTask(ctx, task.ID)
		if err != nil {
			return err
		}
		if current.Status != domain.TaskRunning || current.ProcessRunID != task.ProcessRunID {
			return nil
		}
		current.Status = domain.TaskFailed
		current.Error = result.FailureReason
		current.FinishedAt = &now
		current.UpdatedAt = now
		if err := tx.MindMaps().SaveTask(ctx, current); err != nil {
			return err
		}
		return tx.Processes().MarkExited(ctx, processdomain.RunID(task.ProcessRunID), result)
	})
	if err != nil {
		log.Printf("fail mind map task: task=%s error=%v", task.ID, err)
	}
	q.Schedule()
}

func (q *Queue) recoverRunning(ctx context.Context) error {
	tasks, err := q.repo.ListTasks(ctx, "")
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Status != domain.TaskRunning {
			continue
		}
		now := q.now()
		if task.ProcessRunID != "" {
			_ = q.processes.MarkExited(ctx, processdomain.RunID(task.ProcessRunID), processdomain.ExitResult{FailureReason: "service_restarted", FinishedAt: now})
		}
		task.Status = domain.TaskQueued
		task.ProcessRunID = ""
		task.Error = ""
		task.StartedAt = nil
		task.FinishedAt = nil
		task.UpdatedAt = now
		if err := q.repo.SaveTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

func asyncTaskPrompt(session sessiondomain.Session) string {
	requirement := strings.TrimSpace(session.Requirement)
	if requirement == "" {
		return "整理当前已关闭会话与项目思维图。"
	}
	return "整理当前已关闭会话与项目思维图。原始需求：\n" + requirement
}

func generateProcessRunID() (processdomain.RunID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return processdomain.RunID(hex.EncodeToString(value[:])), nil
}
