import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout'
import { RequireAuth } from './components/RequireAuth'
import { Agents } from './pages/Agents'
import { DeviceUsage } from './pages/DeviceUsage'
import { Devices } from './pages/Devices'
import { Downloads } from './pages/Downloads'
import { Events } from './pages/Events'
import { Login } from './pages/Login'
import { Models } from './pages/Models'
import { Overview } from './pages/Overview'
import { Pricing } from './pages/Pricing'
import { Projects } from './pages/Projects'
import { AuthProvider } from './state/AuthContext'
import { FiltersProvider } from './state/FiltersContext'

function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route
            element={
              <RequireAuth>
                <FiltersProvider>
                  <Layout />
                </FiltersProvider>
              </RequireAuth>
            }
          >
            <Route path="/" element={<Overview />} />
            <Route path="/agents" element={<Agents />} />
            <Route path="/models" element={<Models />} />
            <Route path="/projects" element={<Projects />} />
            {/* 기기 (usage analytics) and 기기 관리 (administration) are separate top-level entries. */}
            <Route path="/devices" element={<DeviceUsage />} />
            <Route path="/manage/devices" element={<Devices />} />
            <Route path="/devices/manage" element={<Navigate to="/manage/devices" replace />} />
            <Route path="/downloads-page" element={<Downloads />} />
            <Route path="/events" element={<Events />} />
            <Route path="/pricing" element={<Pricing />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  )
}

export default App
