// The manage-accounts view: a page of its own (not a modal) holding the
// account list and the add/edit form, with a way back to the dashboard.
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { AccountForm } from "./account-form";
import { ApiError, deleteAccount, listAccounts } from "./api";
import { t } from "./i18n";
import { Icon } from "./icons";
import type { AccountList, AccountView, OverviewAccount } from "./types";

/** What the view shows. `back` is where cancelling the form returns to. */
export type ManageTarget = { kind: "list" } | { kind: "form"; id: string | null; focusCredential?: boolean; back: "dashboard" | "list" };

function hostOf(base: string): string {
  try {
    return new URL(base).host;
  } catch {
    return base;
  }
}

function DeleteDialog(p: { apiKey: string; account: AccountView; onDeleted: () => void; onClose: () => void; onUnauthorized: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);

  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      await deleteAccount(p.apiKey, p.account.id);
      p.onDeleted();
    } catch (e) {
      setBusy(false);
      if (e instanceof ApiError && e.status === 401) p.onUnauthorized();
      else if (e instanceof ApiError && e.body?.code === "account_not_found") p.onDeleted();
      else setError(e instanceof ApiError ? t("delete.failedHttp", { status: e.status }) : t("delete.failedNetwork"));
    }
  };

  return (
    <dialog
      ref={ref}
      class="nm-dialog"
      aria-labelledby="nm-delete-title"
      aria-describedby="nm-delete-text"
      onClose={p.onClose}
      onCancel={(e) => {
        if (busy) e.preventDefault();
      }}
    >
      <h2 class="nm-dialog-title" id="nm-delete-title">
        {t("delete.title", { name: p.account.name })}
      </h2>
      <p class="nm-dialog-text" id="nm-delete-text">
        {t("delete.text")}
      </p>
      {error && (
        <p class="nm-form-error" role="alert">
          <Icon name="alert" />
          {error}
        </p>
      )}
      <div class="nm-form-actions">
        <button type="button" class="nm-btn nm-btn-danger" onClick={() => void confirm()} disabled={busy} aria-busy={busy}>
          {busy ? t("delete.deleting") : t("delete.confirm")}
        </button>
        {/* autofocus: the safe choice is the default */}
        <button type="button" class="nm-btn" autoFocus onClick={() => ref.current?.close()} disabled={busy}>
          {t("delete.cancel")}
        </button>
      </div>
    </dialog>
  );
}

function AccountTable(p: {
  list: AccountList;
  overview: OverviewAccount[];
  onAdd: () => void;
  onEdit: (id: string) => void;
  onDelete: (a: AccountView) => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useLayoutEffect(() => heading.current?.focus(), []);
  const states = new Map(p.overview.map((a) => [a.id, a.state]));
  const { accounts } = p.list;
  return (
    <>
      <header class="nm-view-head">
        <h2 class="nm-view-title" ref={heading} tabIndex={-1}>
          {t("manage.title")}
        </h2>
        <button type="button" class="nm-btn nm-btn-primary" onClick={p.onAdd}>
          <Icon name="plus" />
          {t("manage.add")}
        </button>
      </header>
      {accounts.length === 0 ? (
        <div class="nm-empty nm-empty--manage">
          <p class="nm-empty-title">{t("manage.empty.title")}</p>
          <p class="nm-empty-text">{t("manage.empty.text")}</p>
        </div>
      ) : (
        <table class="nm-table">
          <thead>
            <tr>
              <th scope="col">{t("manage.col.name")}</th>
              <th scope="col">{t("manage.col.source")}</th>
              <th scope="col">{t("manage.col.site")}</th>
              <th scope="col">{t("manage.col.state")}</th>
              <th scope="col" class="nm-table-actions">
                <span class="nm-visually-hidden">{t("manage.col.actions")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {accounts.map((a) => {
              const state = states.get(a.id);
              return (
                <tr key={a.id}>
                  <th scope="row" class="nm-table-name">
                    {a.name}
                  </th>
                  <td>{t(a.source === "custom" ? "source.custom" : "source.newapi")}</td>
                  <td class="nm-table-host" title={a.base_url}>
                    {hostOf(a.base_url)}
                  </td>
                  <td>
                    {state ? (
                      <span class="nm-state" data-state={state}>
                        <i class="nm-dot" />
                        {t(`state.${state}`)}
                      </span>
                    ) : (
                      <span class="nm-state nm-state--unknown">{t("manage.notRead")}</span>
                    )}
                  </td>
                  <td class="nm-table-actions">
                    <button type="button" class="nm-btn nm-btn-small" aria-label={t("manage.editLabel", { name: a.name })} onClick={() => p.onEdit(a.id)}>
                      {t("manage.edit")}
                    </button>
                    <button
                      type="button"
                      class="nm-btn nm-btn-small nm-btn-quiet-danger"
                      aria-label={t("manage.deleteLabel", { name: a.name })}
                      onClick={() => p.onDelete(a)}
                    >
                      {t("manage.delete")}
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {p.list.data_file && (
        <p class="nm-datafile">
          {t("manage.dataFile")} <code class="nm-code">{p.list.data_file}</code>
        </p>
      )}
    </>
  );
}

type ListState = { kind: "loading" } | { kind: "error"; error: unknown } | { kind: "ready"; list: AccountList };

export function Manage(p: {
  apiKey: string;
  target: ManageTarget;
  /** the dashboard's latest reading, for status and display currency */
  overview: OverviewAccount[];
  onTarget: (t: ManageTarget) => void;
  /** back to the dashboard; `savedId` is the account just saved */
  onLeave: (savedId?: string) => void;
  /** an account was deleted: the dashboard must reload */
  onChanged: () => void;
  onUnauthorized: () => void;
}) {
  const [state, setState] = useState<ListState>({ kind: "loading" });
  // One dialog per request: a late close event of an earlier dialog cannot dismiss a new one.
  const [pendingDelete, setPendingDelete] = useState<{ account: AccountView; n: number } | null>(null);
  const requests = useRef(0);
  // Bumped after a deletion: the deleted row's button is gone, so focus the heading.
  const [deletions, setDeletions] = useState(0);
  useLayoutEffect(() => {
    if (deletions > 0) document.querySelector<HTMLElement>(".nm-view-title")?.focus();
  }, [deletions]);

  const load = async () => {
    setState({ kind: "loading" });
    try {
      setState({ kind: "ready", list: await listAccounts(p.apiKey) });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) p.onUnauthorized();
      else setState({ kind: "error", error: e });
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const target = p.target;
  const editing = target.kind === "form" ? target : null;
  let body;
  if (state.kind === "loading") {
    body = (
      <p class="nm-status-line" role="status">
        {t("manage.loading")}
      </p>
    );
  } else if (state.kind === "error") {
    const e = state.error;
    body = (
      <div class="nm-alert" role="alert">
        <p class="nm-alert-title">
          <Icon name="alert" />
          {t("manage.loadFailed")}
        </p>
        <p class="nm-alert-text">{e instanceof ApiError ? t("error.http.text", { status: e.status }) : t("error.network.text")}</p>
        <div class="nm-actions">
          <button type="button" class="nm-btn nm-btn-primary" onClick={() => void load()}>
            <Icon name="refresh" />
            {t("error.retry")}
          </button>
        </div>
      </div>
    );
  } else if (editing && (editing.id === null || state.list.accounts.some((a) => a.id === editing.id))) {
    const { id, back, focusCredential } = editing;
    const stored = state.list.accounts.find((a) => a.id === id) ?? null;
    body = (
      <AccountForm
        key={id ?? "new"}
        apiKey={p.apiKey}
        stored={stored}
        currency={(id && p.overview.find((a) => a.id === id)?.currency) || null}
        focusCredential={focusCredential}
        onSaved={(a) => p.onLeave(a.id)}
        onCancel={() => (back === "dashboard" ? p.onLeave() : p.onTarget({ kind: "list" }))}
        onUnauthorized={p.onUnauthorized}
      />
    );
  } else {
    // The list, also when the account to edit no longer exists.
    const list = state.list;
    body = (
      <AccountTable
        list={list}
        overview={p.overview}
        onAdd={() => p.onTarget({ kind: "form", id: null, back: "list" })}
        onEdit={(id) => p.onTarget({ kind: "form", id, back: "list" })}
        onDelete={(account) => setPendingDelete({ account, n: ++requests.current })}
      />
    );
  }

  return (
    <section class="nm-manage" aria-label={t("manage.title")}>
      <div class="nm-manage-bar">
        <button type="button" class="nm-btn nm-btn-quiet" onClick={() => p.onLeave()}>
          <Icon name="left" />
          {t("manage.back")}
        </button>
      </div>
      {body}
      {pendingDelete && (
        <DeleteDialog
          key={pendingDelete.n}
          apiKey={p.apiKey}
          account={pendingDelete.account}
          onUnauthorized={p.onUnauthorized}
          onClose={() => setPendingDelete((cur) => (cur === pendingDelete ? null : cur))}
          onDeleted={() => {
            const gone = pendingDelete.account.id;
            setPendingDelete(null);
            setState((s) => (s.kind === "ready" ? { kind: "ready", list: { ...s.list, accounts: s.list.accounts.filter((a) => a.id !== gone) } } : s));
            p.onChanged();
            setDeletions((n) => n + 1);
          }}
        />
      )}
    </section>
  );
}
