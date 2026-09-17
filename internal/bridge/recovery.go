package bridge

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"

	"omp-telegram/internal/store"
	"omp-telegram/internal/telegram"
)

func (b *Bridge) launchWorker(ctx context.Context, key target, binding store.Binding, restoring bool) *worker {
	ctx, cancel := context.WithCancel(ctx)
	w := &worker{
		b: b, key: key, binding: binding, restoring: restoring,
		input:    make(chan incoming, b.cfg.QueueCapacity+16),
		confirms: map[string]confirmation{}, previewResult: make(chan previewResult, 1),
		operations: make(chan operationResult, 1), ctx: ctx, cancel: cancel,
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		w.run()
	}()
	return w
}

func (b *Bridge) restoreWorkers(ctx context.Context, workers map[target]*worker) error {
	bindings, err := b.db.RunningBindings(b.bot.ID)
	if err != nil {
		return err
	}
	// Snapshot before polling starts: restoring an instance must not restart old prompts.
	pending, err := b.db.Pending()
	if err != nil {
		return err
	}
	for _, in := range pending {
		var update telegram.Update
		if json.Unmarshal(in.Raw, &update) != nil || update.Message == nil {
			continue
		}
		message := update.Message
		if len(message.Photo) != 0 || message.Document != nil || !strings.HasPrefix(strings.TrimSpace(message.Text), "/") {
			if err := b.db.Mark(in.ID, "cancelled"); err != nil {
				return err
			}
		}
	}
	for _, binding := range bindings {
		if ctx.Err() != nil {
			break
		}
		if binding.Thread == 0 || !slices.Contains(b.cfg.AllowedChats, binding.Chat) {
			continue
		}
		key := target{binding.Chat, binding.Thread}
		workers[key] = b.launchWorker(ctx, key, binding, true)
	}
	return nil
}

func sameSessionFile(left, right string) bool {
	a, err := os.Stat(left)
	if err != nil {
		return false
	}
	b, err := os.Stat(right)
	return err == nil && os.SameFile(a, b)
}

func (w *worker) persistClosed() bool {
	if !w.binding.Running {
		return true
	}
	if err := w.b.db.SetRunning(w.binding, false); err != nil {
		w.b.fail(err)
		return false
	}
	w.binding.Running = false
	return true
}
