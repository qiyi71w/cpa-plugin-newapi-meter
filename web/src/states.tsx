// Whole-page states: loading, no accounts, Management Key input, load errors.
import { useState } from "preact/hooks";
import { ApiError } from "./api";
import { t, tDynamic } from "./i18n";
import { Icon } from "./icons";

export function Loading() {
  return (
    <div class="nm-loading" aria-busy="true">
      <div class="nm-band nm-band--skeleton" aria-hidden="true">
        {[0, 1, 2].map((i) => (
          <div key={i} class="nm-skel-tab">
            <span class="nm-skel nm-skel-name" />
            <span class="nm-skel nm-skel-bal" />
            <span class="nm-skel nm-skel-sub" />
          </div>
        ))}
      </div>
      <p class="nm-status-line" role="status">
        {t("loading.text")}
      </p>
    </div>
  );
}

export const EMPTY_ADD_ID = "nm-empty-add";

export function Empty(p: { onAdd: () => void }) {
  return (
    <section class="nm-empty">
      <h2 class="nm-empty-title">{t("empty.title")}</h2>
      <p class="nm-empty-text">{t("empty.text")}</p>
      <div class="nm-actions">
        <button type="button" id={EMPTY_ADD_ID} class="nm-btn nm-btn-primary" onClick={p.onAdd}>
          <Icon name="plus" />
          {t("empty.add")}
        </button>
      </div>
    </section>
  );
}

export function KeyPrompt(p: { rejected: boolean; onSubmit: (key: string) => void }) {
  const [value, setValue] = useState("");
  return (
    <form
      class="nm-keyform"
      onSubmit={(e) => {
        e.preventDefault();
        const key = value.trim();
        if (key) p.onSubmit(key);
      }}
    >
      <h2 class="nm-keyform-title">{t("key.title")}</h2>
      <p class="nm-keyform-text" role={p.rejected ? "alert" : undefined}>
        {t(p.rejected ? "key.rejected" : "key.missing")}
      </p>
      <label class="nm-field-label" for="nm-key">
        {t("key.label")}
      </label>
      <div class="nm-keyform-row">
        <input
          id="nm-key"
          class="nm-input"
          type="password"
          autocomplete="off"
          spellcheck={false}
          autoFocus
          value={value}
          onInput={(e) => setValue(e.currentTarget.value)}
        />
        <button type="submit" class="nm-btn nm-btn-primary" disabled={value.trim() === ""}>
          {t("key.submit")}
        </button>
      </div>
      <p class="nm-field-hint">{t("key.note")}</p>
    </form>
  );
}

export function LoadError(p: { error: unknown; onRetry: () => void }) {
  let title: string;
  let text: string;
  let path: string | undefined;
  const err = p.error;
  if (err instanceof ApiError && err.body?.code?.startsWith("data_file_")) {
    const code = err.body.code;
    title = tDynamic(`error.${code}.title`, t("error.http.title"));
    text = tDynamic(`error.${code}.text`, t("error.http.text", { status: err.status }));
    path = err.body.path;
  } else if (err instanceof ApiError) {
    title = t("error.http.title");
    text = t("error.http.text", { status: err.status });
  } else {
    title = t("error.network.title");
    text = t("error.network.text");
  }
  return (
    <div class="nm-alert" role="alert">
      <p class="nm-alert-title">
        <Icon name="alert" />
        {title}
      </p>
      <p class="nm-alert-text">{text}</p>
      {path && (
        <p class="nm-alert-meta">
          {t("error.path")} <code class="nm-code">{path}</code>
        </p>
      )}
      <div class="nm-actions">
        <button type="button" class="nm-btn nm-btn-primary" onClick={p.onRetry}>
          <Icon name="refresh" />
          {t("error.retry")}
        </button>
      </div>
    </div>
  );
}
