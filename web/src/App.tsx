import { Link, Route, Routes } from 'react-router-dom'
import HostsPage from './pages/HostsPage'
import HostDetailPage from './pages/HostDetailPage'
import AlertsPage from './pages/AlertsPage'
import QuotaPage from './pages/QuotaPage'
import OpsPasswordModal from './OpsPasswordModal'

export default function App() {
  return (
    <div className="app">
      <header className="header">
        <Link to="/" className="brand">ResourceHub</Link>
        <nav>
          <Link to="/">总览</Link>
          <Link to="/alerts">告警</Link>
          <Link to="/quota">软配额</Link>
        </nav>
      </header>
      <main className="main">
        <Routes>
          <Route path="/" element={<HostsPage />} />
          <Route path="/hosts/:id" element={<HostDetailPage />} />
          <Route path="/alerts" element={<AlertsPage />} />
          <Route path="/quota" element={<QuotaPage />} />
        </Routes>
      </main>
      <OpsPasswordModal />
    </div>
  )
}
