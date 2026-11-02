import { useQuery } from "@tanstack/react-query";
import { Navigate, NavLink, Outlet, Route, Routes, useLocation } from "react-router";

import { api, ApiError, meKey, setupKey, type Me } from "./api/client";
import { useLiveEvents } from "./api/events";
import { AlbumPage } from "./pages/AlbumPage";
import { CatalogPage } from "./pages/CatalogPage";
import { DevicesPage } from "./pages/DevicesPage";
import { DiaryPage } from "./pages/DiaryPage";
import { InboxPage, useInbox } from "./pages/InboxPage";
import { LibraryPage } from "./pages/LibraryPage";
import { LoginPage } from "./pages/LoginPage";
import { NowPlayingPage } from "./pages/NowPlayingPage";
import { SettingsPage } from "./pages/SettingsPage";
import { SetupPage } from "./pages/SetupPage";
import { StatsPage } from "./pages/StatsPage";

export function App() {
  return (
    <Routes>
      <Route path="/setup" element={<SetupPage />} />
      <Route path="/login" element={<LoginPage />} />
      <Route element={<RequireAccount />}>
        <Route index element={<NowPlayingPage />} />
        <Route path="inbox" element={<InboxPage />} />
        <Route path="library" element={<LibraryPage />} />
        <Route path="albums/:id" element={<AlbumPage />} />
        <Route path="diary" element={<DiaryPage />} />
        <Route path="stats" element={<StatsPage />} />
        <Route path="catalog" element={<CatalogPage />} />
        <Route path="devices" element={<DevicesPage />} />
        <Route path="settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function RequireAccount() {
  const location = useLocation();
  const setup = useQuery({ queryKey: setupKey, queryFn: () => api<{ needed: boolean }>("/api/setup") });
  const me = useQuery({
    queryKey: meKey,
    queryFn: () => api<Me>("/api/me"),
    enabled: setup.data?.needed === false,
    retry: false,
  });

  if (setup.data?.needed) return <Navigate to="/setup" replace />;
  if (me.error instanceof ApiError && me.error.status === 401) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }
  if (me.data) return <Layout me={me.data} />;
  if (setup.error || me.error) {
    return (
      <section className="empty">
        <h1>Can't reach Spindle</h1>
        <p className="muted">{(setup.error ?? me.error)?.message}</p>
        <button onClick={() => (setup.error ? setup.refetch() : me.refetch())}>Try again</button>
      </section>
    );
  }
  return <p className="loading">Loading…</p>;
}

function Layout({ me }: { me: Me }) {
  useLiveEvents();
  const inbox = useInbox();
  const waiting = inbox.data?.length ?? 0;
  return (
    <div className="shell">
      <a
        className="skip"
        href="#main"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("main")?.focus();
        }}
      >
        Skip to content
      </a>
      <header>
        <span className="brand">Spindle</span>
        <nav className="tabbar" aria-label="Main">
          <NavLink to="/" end>
            Now playing
          </NavLink>
          <NavLink to="/inbox">
            Inbox
            {waiting > 0 && (
              <>
                <span className="badge" aria-hidden="true">
                  {waiting}
                </span>
                <span className="sr-only">, {waiting} waiting</span>
              </>
            )}
          </NavLink>
          <NavLink to="/library">Library</NavLink>
          <NavLink to="/diary">Diary</NavLink>
          <NavLink to="/stats">Stats</NavLink>
        </nav>
        <nav className="utility" aria-label="Collection and account">
          <NavLink to="/catalog">Catalogue</NavLink>
          <NavLink to="/devices">Stands</NavLink>
          <NavLink to="/settings" className="account">
            {me.username}
          </NavLink>
        </nav>
      </header>
      <main id="main" tabIndex={-1}>
        <Outlet />
      </main>
    </div>
  );
}
