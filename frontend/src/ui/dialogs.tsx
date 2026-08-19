import {useCallback, useEffect, useRef, useState} from 'react';

// In-app replacements for window.confirm / window.prompt.
//
// Wails' macOS WebView (WailsContext) declares WKUIDelegate conformance and is
// installed as the webview's UIDelegate, but it implements none of the
// runJavaScriptAlertPanel / ConfirmPanel / TextInputPanel methods. WebKit treats
// those as optional: when they are missing the panel is never shown and the call
// returns the dismissed default immediately — confirm() yields false and
// prompt() yields null. Every guarded action therefore became a silent no-op in
// the packaged app while still working in a browser during `wails dev`.
//
// These dialogs are rendered by the app itself, so they behave identically in
// both environments and pick up the workspace theme.

export type ConfirmOptions = {
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  destructive?: boolean;
};

export type PromptOptions = {
  title: string;
  message?: string;
  defaultValue?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  placeholder?: string;
  multiline?: boolean;
};

type Request =
  | (ConfirmOptions & {kind: 'confirm'; resolve: (value: boolean) => void})
  | (PromptOptions & {kind: 'prompt'; resolve: (value: string | null) => void});

// The host registers itself here so any module can raise a dialog without the
// callers having to thread props through the component tree.
let present: ((request: Request) => void) | null = null;

export function confirmDialog(options: ConfirmOptions): Promise<boolean> {
  if (!present) {
    return Promise.resolve(window.confirm(dialogText(options.title, options.message)));
  }
  return new Promise<boolean>((resolve) => {
    present?.({...options, kind: 'confirm', resolve});
  });
}

export function promptDialog(options: PromptOptions): Promise<string | null> {
  if (!present) {
    return Promise.resolve(window.prompt(dialogText(options.title, options.message), options.defaultValue ?? ''));
  }
  return new Promise<string | null>((resolve) => {
    present?.({...options, kind: 'prompt', resolve});
  });
}

function dialogText(title: string, message?: string): string {
  return message ? `${title}\n\n${message}` : title;
}

export function DialogHost() {
  const [request, setRequest] = useState<Request | null>(null);
  const [value, setValue] = useState('');
  const inputRef = useRef<HTMLInputElement | HTMLTextAreaElement | null>(null);
  const confirmRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    present = (next) => {
      setValue(next.kind === 'prompt' ? next.defaultValue ?? '' : '');
      setRequest(next);
    };
    return () => {
      present = null;
    };
  }, []);

  // Settle the promise exactly once, so a caller awaiting it can never hang.
  const settle = useCallback((accepted: boolean, text: string) => {
    setRequest((current) => {
      if (!current) {
        return null;
      }
      if (current.kind === 'confirm') {
        current.resolve(accepted);
      } else {
        current.resolve(accepted ? text : null);
      }
      return null;
    });
  }, []);

  useEffect(() => {
    if (!request) {
      return;
    }
    const focus = window.setTimeout(() => {
      if (request.kind === 'prompt') {
        inputRef.current?.focus();
        inputRef.current?.select();
      } else {
        confirmRef.current?.focus();
      }
    }, 0);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        settle(false, '');
      }
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => {
      window.clearTimeout(focus);
      window.removeEventListener('keydown', onKeyDown, true);
    };
  }, [request, settle]);

  if (!request) {
    return null;
  }

  const isPrompt = request.kind === 'prompt';
  const confirmLabel = request.confirmLabel || (isPrompt ? 'OK' : 'Confirm');
  const destructive = request.kind === 'confirm' && request.destructive;

  return (
    <div className="gm-dialog-scrim" onMouseDown={() => settle(false, '')}>
      <div
        className="gm-dialog"
        role="dialog"
        aria-modal="true"
        aria-label={request.title}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <h2 className="gm-dialog-title">{request.title}</h2>
        {request.message && <p className="gm-dialog-message">{request.message}</p>}
        {isPrompt && (
          request.multiline ? (
            <textarea
              ref={(node) => { inputRef.current = node; }}
              className="gm-dialog-input gm-dialog-textarea"
              value={value}
              placeholder={request.placeholder}
              onChange={(event) => setValue(event.target.value)}
            />
          ) : (
            <input
              ref={(node) => { inputRef.current = node; }}
              className="gm-dialog-input"
              value={value}
              placeholder={request.placeholder}
              onChange={(event) => setValue(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') {
                  event.preventDefault();
                  settle(true, value);
                }
              }}
            />
          )
        )}
        <div className="gm-dialog-actions">
          <button type="button" className="gm-btn gm-btn-sm gm-btn-ghost" onClick={() => settle(false, '')}>
            {request.cancelLabel || 'Cancel'}
          </button>
          <button
            ref={confirmRef}
            type="button"
            className={destructive ? 'gm-btn gm-btn-sm gm-btn-danger' : 'gm-btn gm-btn-sm gm-btn-primary'}
            onClick={() => settle(true, value)}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

export default DialogHost;
