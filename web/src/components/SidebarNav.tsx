import { NavLink } from 'react-router-dom'
import { LedgerStatus } from './LedgerStatus'

const items = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/entries', label: 'Entries', end: false },
  { to: '/export', label: 'Export', end: false },
  { to: '/settings', label: 'Settings', end: false },
]

export function SidebarNav() {
  return (
    <aside className="on-dark flex flex-col bg-sidebar px-4 py-6 text-text-on-dark min-[801px]:sticky min-[801px]:top-0 min-[801px]:h-screen min-[801px]:flex-[0_0_240px] max-[800px]:flex-[1_0_100%]">
      <div className="mb-6 flex items-center gap-3 px-2">
        <span aria-hidden="true" className="grid h-9 w-9 place-items-center rounded-control bg-accent font-display text-lg font-bold">K</span>
        <span className="font-display text-xl font-semibold">Kira</span>
      </div>
      <nav aria-label="Main" className="flex flex-col gap-1.5 max-[800px]:flex-row max-[800px]:flex-wrap">
        {items.map((i) => (
          <NavLink
            key={i.to}
            to={i.to}
            end={i.end}
            className={({ isActive }) =>
              `flex h-control items-center rounded-control px-3 text-[15px] font-medium no-underline ${
                isActive ? 'bg-sidebar-active text-white' : 'text-text-on-dark-muted hover:bg-sidebar-active/60 hover:text-white'
              }`
            }
          >
            {i.label}
          </NavLink>
        ))}
      </nav>
      <div className="mt-auto pt-6"><LedgerStatus /></div>
    </aside>
  )
}
