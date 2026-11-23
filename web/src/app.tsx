import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { ApiError, getOverview, type OverviewOptions } from "./api";
import { readSavedKey } from "./auth";
import { PANEL_ID, TabBand, tabId } from "./band";
import { Detail, MANAGE_BUTTON_ID } from "./detail";
import { t } from "./i18n";
import { Manage, type ManageTarget } from "./manage";
import { defaultSelection } from "./model";
import { Empty, EMPTY_ADD_ID, KeyPrompt, Loading, LoadError } from "./states";
import type { Overview } from "./types";

type Phase = { kind: "loading" } | { kind: "key"; rejected: boolean } | { kind: "error"; error: unknown } | { kind: "ready" };

/** The dashboard, or the manage-accounts view in its place. */
type View = { kind: "dashboard" } | { kind: "manage"; target: ManageTarget };

/** Re-render periodically so relative times ("3 分钟前") stay current. */
function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function App() {
  // A key typed into the page lives only here, in memory.
  const key = useRef<string | null>(readSavedKey());
  const [phase, setPhase] = useState<Phase>(key.current ? { kind: "loading" } : { kind: "key", rejected: false });
  const [data, setData] = useState<Overview | null>(null);
  const hasData = useRef(false);
  const [selected, setSelected] = useState<string | null>(null);
  // What is being refreshed: every account, or one account's retry.
  const [busy, setBusy] = useState<{ all: true } | { id: string } | null>(null);
  const [refreshFailed, setRefreshFailed] = useState(false);
  const lastShown = useRef<string | null>(null);
  const nowMs = useNow(15_000);
  const [view, setView] = useState<View>({ kind: "dashboard" });
  // An account was deleted while managing: the dashboard reloads on return.
  const stale = useRef(false);
  // Element to focus once the dashboard is back.
  const returnFocus = useRef<string | null>(null);

  const load = async (opts: OverviewOptions) => {
    if (!key.current) {
      setPhase({ kind: "key", rejected: false });
      return;
    }
    try {
      const ov = await getOverview(key.current, opts);
      hasData.current = true;
      setData(ov);
      setRefreshFailed(false);
      setSelected((cur) => (cur && ov.accounts.some((a) => a.id === cur) ? cur : defaultSelection(ov.accounts)));
      setPhase({ kind: "ready" });
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        unauthorized();
      } else if (hasData.current && !(e instanceof ApiError && e.body?.code?.startsWith("data_file_"))) {
        setRefreshFailed(true);
      } else {
        setPhase({ kind: "error", error: e });
      }
    }
  };

  const refresh = async (scope: { all: true } | { id: string }) => {
    setBusy(scope);
    await load("id" in scope ? { force: true, id: scope.id } : { force: true });
    setBusy(null);
  };

  useLayoutEffect(() => {
    const want = returnFocus.current;
    if (view.kind !== "dashboard" || !want || busy) return;
    // Back to where the user left, or to the empty state's entry when no account is left.
    const el = document.getElementById(want) ?? document.getElementById(EMPTY_ADD_ID);
    if (!el) return;
    el.focus();
    returnFocus.current = null;
  });

  const manage = (target: ManageTarget) => setView({ kind: "manage", target });

  /** Back from managing; a saved account becomes the selected tab. */
  const leaveManage = async (savedId?: string) => {
    setView({ kind: "dashboard" });
    returnFocus.current = savedId ? tabId(savedId) : MANAGE_BUTTON_ID;
    if (savedId) setSelected(savedId);
    const changed = !!savedId || stale.current;
    stale.current = false;
    if (!changed) return;
    // A first account has no tab yet: show loading rather than the empty state.
    if (!data || data.accounts.length === 0) setPhase({ kind: "loading" });
    setBusy({ all: true });
    await load({});
    setBusy(null);
  };

  const unauthorized = () => {
    key.current = null;
    setView({ kind: "dashboard" });
    setPhase({ kind: "key", rejected: true });
  };

  useEffect(() => {
    document.title = t("page.title");
    if (key.current) void load({});
  }, []);

  let body;
  if (view.kind === "manage" && phase.kind === "ready" && key.current) {
    body = (
      <Manage
        apiKey={key.current}
        target={view.target}
        overview={data?.accounts ?? []}
        onTarget={manage}
        onLeave={(id) => void leaveManage(id)}
        onChanged={() => {
          stale.current = true;
        }}
        onUnauthorized={unauthorized}
      />
    );
  } else if (phase.kind === "loading") {
    body = <Loading />;
  } else if (phase.kind === "key") {
    body = (
      <KeyPrompt
        rejected={phase.rejected}
        onSubmit={(k) => {
          key.current = k;
          setPhase({ kind: "loading" });
          void load({});
        }}
      />
    );
  } else if (phase.kind === "error") {
    body = (
      <LoadError
        error={phase.error}
        onRetry={() => {
          setPhase({ kind: "loading" });
          void load({});
        }}
      />
    );
  } else if (!data || data.accounts.length === 0) {
    body = <Empty onAdd={() => manage({ kind: "form", id: null, back: "dashboard" })} />;
  } else {
    const account = data.accounts.find((a) => a.id === selected) ?? data.accounts[0];
    // The panel fades up only when the user moves to another account.
    const entering = lastShown.current !== null && lastShown.current !== account.id;
    lastShown.current = account.id;
    body = (
      <>
        {refreshFailed && (
          <p class="nm-banner" role="status">
            {t("error.refreshFailed")}
          </p>
        )}
        <TabBand accounts={data.accounts} selected={account.id} nowMs={nowMs} onSelect={setSelected} />
        <section class="nm-panel" id={PANEL_ID} role="tabpanel" aria-labelledby={tabId(account.id)}>
          <div class={entering ? "nm-panel-in nm-enter" : "nm-panel-in"} key={account.id}>
            <Detail
              a={account}
              nowMs={nowMs}
              refreshing={busy !== null && "all" in busy}
              retrying={busy !== null && "id" in busy && busy.id === account.id}
              onRefresh={() => void refresh({ all: true })}
              onRetry={() => void refresh({ id: account.id })}
              onManage={() => manage({ kind: "list" })}
              onReplaceCredential={() => manage({ kind: "form", id: account.id, focusCredential: true, back: "dashboard" })}
            />
          </div>
        </section>
      </>
    );
  }
  return <main class="nm">{body}</main>;
}
