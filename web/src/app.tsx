import { useEffect, useRef, useState } from "preact/hooks";
import { ApiError, getOverview, type OverviewOptions } from "./api";
import { readSavedKey } from "./auth";
import { PANEL_ID, TabBand, tabId } from "./band";
import { Detail } from "./detail";
import { t } from "./i18n";
import { defaultSelection } from "./model";
import { Empty, KeyPrompt, Loading, LoadError } from "./states";
import type { Overview } from "./types";

type Phase = { kind: "loading" } | { kind: "key"; rejected: boolean } | { kind: "error"; error: unknown } | { kind: "ready" };

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
        key.current = null;
        setPhase({ kind: "key", rejected: true });
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

  useEffect(() => {
    document.title = t("page.title");
    if (key.current) void load({});
  }, []);

  let body;
  if (phase.kind === "loading") {
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
    body = <Empty />;
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
            />
          </div>
        </section>
      </>
    );
  }
  return <main class="nm">{body}</main>;
}
