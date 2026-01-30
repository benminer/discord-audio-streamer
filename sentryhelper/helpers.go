// Package sentryhelper provides utilities for Sentry transaction and scope management.
// It ensures proper isolation of breadcrumbs and context per Discord command.
package sentryhelper

import (
	"context"
	"fmt"

	sentry "github.com/getsentry/sentry-go"
)

// contextKey is used to store the cloned hub in context
type contextKey string

const hubContextKey contextKey = "sentry_hub"

// StartCommandTransaction creates a new transaction with a cloned hub for a Discord command.
// The cloned hub ensures breadcrumbs and scope are isolated to this command only.
// Returns the context with the transaction and hub, plus the transaction span.
func StartCommandTransaction(ctx context.Context, commandName string, guildID string, userID string) (context.Context, *sentry.Span) {
	// Clone the hub to isolate scope (breadcrumbs, tags, user context)
	hub := sentry.CurrentHub().Clone()

	// Put the cloned hub into the context
	ctx = context.WithValue(ctx, hubContextKey, hub)

	// Start the transaction
	transactionName := fmt.Sprintf("discord.command.%s", commandName)
	transaction := sentry.StartTransaction(ctx, transactionName,
		sentry.WithOpName("discord.command"),
		sentry.WithTransactionSource(sentry.SourceRoute),
	)

	// Set initial tags on the transaction
	transaction.SetTag("command", commandName)
	transaction.SetTag("guild_id", guildID)
	transaction.SetTag("user_id", userID)

	// Bind the transaction to the cloned hub's scope
	hub.Scope().SetSpan(transaction)

	return transaction.Context(), transaction
}

// HubFromContext retrieves the cloned hub from context.
// Falls back to CurrentHub if no cloned hub is found (for backward compatibility).
func HubFromContext(ctx context.Context) *sentry.Hub {
	if ctx == nil {
		return sentry.CurrentHub()
	}
	if hub, ok := ctx.Value(hubContextKey).(*sentry.Hub); ok && hub != nil {
		return hub
	}
	return sentry.CurrentHub()
}

// AddBreadcrumb adds a breadcrumb to the hub in context (isolated per-command).
func AddBreadcrumb(ctx context.Context, breadcrumb *sentry.Breadcrumb) {
	hub := HubFromContext(ctx)
	hub.AddBreadcrumb(breadcrumb, nil)
}

// CaptureException captures an exception on the hub in context.
func CaptureException(ctx context.Context, err error) *sentry.EventID {
	hub := HubFromContext(ctx)
	return hub.CaptureException(err)
}

// CaptureMessage captures a message on the hub in context.
// Use this for warnings or informational events that aren't errors.
func CaptureMessage(ctx context.Context, message string) *sentry.EventID {
	hub := HubFromContext(ctx)
	return hub.CaptureMessage(message)
}

// ConfigureScope configures the scope on the hub in context.
func ConfigureScope(ctx context.Context, f func(*sentry.Scope)) {
	hub := HubFromContext(ctx)
	hub.ConfigureScope(f)
}

// StartSpan starts a child span attached to the transaction in context.
// If no transaction exists in context, creates an orphaned span (for backward compatibility).
func StartSpan(ctx context.Context, operation string) *sentry.Span {
	return sentry.StartSpan(ctx, operation)
}

// DetachFromTransaction creates a new context that preserves the cloned hub
// but removes the transaction association. Use this for queue items that may
// be processed long after the original command transaction has finished.
// Breadcrumbs and scope will still be isolated to the command's hub.
func DetachFromTransaction(ctx context.Context) context.Context {
	hub := HubFromContext(ctx)
	// Create a fresh context with only the hub, no transaction
	return context.WithValue(context.Background(), hubContextKey, hub)
}

// StartLinkedTransaction creates a new transaction that references an original
// command via tags. Use this for long-running operations (like audio loading/playback)
// that happen after the original command transaction has finished.
func StartLinkedTransaction(ctx context.Context, name string, operation string, commandName string, guildID string) (context.Context, *sentry.Span) {
	hub := HubFromContext(ctx)

	transaction := sentry.StartTransaction(ctx, name,
		sentry.WithOpName(operation),
		sentry.WithTransactionSource(sentry.SourceTask),
	)

	transaction.SetTag("original_command", commandName)
	transaction.SetTag("guild_id", guildID)

	// Bind the transaction to the hub's scope
	hub.Scope().SetSpan(transaction)

	return transaction.Context(), transaction
}
