import { useEffect, useState } from "react";
import { hasPendingWrites } from "./api";

export function useAppUpdates() {
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (!("serviceWorker" in navigator)) return;
    let registration: ServiceWorkerRegistration | undefined;
    let installing: ServiceWorker | null = null;
    let controlled = !!navigator.serviceWorker.controller;
    let reloadPending = false;
    let activating = false;
    let reloading = false;
    let disposed = false;
    const apply = () => {
      if (disposed || reloading || (!reloadPending && !registration?.waiting))
        return;
      setPending(true);
      if (
        !navigator.onLine ||
        hasPendingWrites() ||
        document.querySelector(
          'form[data-dirty="true"], form[data-busy="true"]',
        )
      )
        return;
      if (reloadPending) {
        reloading = true;
        document.documentElement.dataset.allowReload = "true";
        window.location.reload();
      } else if (!activating) {
        activating = true;
        registration?.waiting?.postMessage({ type: "SKIP_WAITING" });
      }
    };
    const changed = () => {
      // The initial installation claims the page without requiring a reload.
      if (!controlled) {
        controlled = true;
        return;
      }
      reloadPending = true;
      apply();
    };
    const watch = () => {
      installing?.removeEventListener("statechange", apply);
      installing = registration?.installing ?? null;
      installing?.addEventListener("statechange", apply);
      apply();
    };
    const check = () => {
      if (!document.hidden) void registration?.update().catch(() => {});
      apply();
    };
    navigator.serviceWorker.addEventListener("controllerchange", changed);
    void navigator.serviceWorker
      .register("/sw.js", { updateViaCache: "none" })
      .then((reg) => {
        if (disposed) return;
        registration = reg;
        reg.addEventListener("updatefound", watch);
        watch();
      })
      .catch(() => {
        /* Offline/update failures do not interrupt work. */
      });
    const retry = setInterval(apply, 1000);
    const poll = setInterval(check, 60000);
    window.addEventListener("online", check);
    window.addEventListener("focus", check);
    document.addEventListener("visibilitychange", check);
    return () => {
      disposed = true;
      clearInterval(retry);
      clearInterval(poll);
      navigator.serviceWorker.removeEventListener("controllerchange", changed);
      registration?.removeEventListener("updatefound", watch);
      installing?.removeEventListener("statechange", apply);
      window.removeEventListener("online", check);
      window.removeEventListener("focus", check);
      document.removeEventListener("visibilitychange", check);
    };
  }, []);
  return pending;
}
