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
  if (!me.data) return <p className="loading">Loading…</p>;
  return <Layout me={me.data} />;
}

function Layout({ me }: { me: Me }) {
  useLiveEvents(true);
  const inbox = useInbox();
  const waiting = inbox.data?.length ?? 0;
  return (
    <div className="shell">
      <header>
        <span className="brand">Spindle</span>
        <nav>
          <NavLink to="/" end>
            Now playing
          </NavLink>
          <NavLink to="/inbox">
            Inbox{waiting > 0 && <span className="badge">{waiting}</span>}
          </NavLink>
          <NavLink to="/library">Library</NavLink>
          <NavLink to="/diary">Diary</NavLink>
          <NavLink to="/catalog">Catalogue</NavLink>
          <NavLink to="/devices">Stands</NavLink>
          <NavLink to="/settings">{me.username}</NavLink>
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  );
}
