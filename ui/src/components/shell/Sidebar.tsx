import { Tooltip } from '@mantine/core';
import { Link } from '@tanstack/react-router';
import {
  Building2,
  ChevronsLeft,
  ChevronsRight,
  ClipboardList,
  FolderGit2,
  LayoutGrid,
  Network,
  Play,
  ShieldCheck,
  Table2,
  Users,
  Zap,
  type LucideIcon,
} from 'lucide-react';
import { useAppStore } from '../../store/useAppStore';
import classes from './Sidebar.module.css';

/**
 * Primary navigation.
 *
 * The previous version listed ten destinations flat, in no order a user could
 * predict — Dashboard, Organizations, Projects, Inbox, Tasks, Instances,
 * Models, Connectors, Users, Groups. A person doing their daily work and a
 * person administering the platform saw the same undifferentiated list, and
 * neither could find their half of it quickly.
 *
 * Grouping here follows what someone came to *do*, not how the backend is
 * organised:
 *
 *   Work      — what needs me today (the majority of sessions end here)
 *   Build     — designing processes and decisions
 *   Operate   — watching what is running
 *   Administer — people and tenancy, visited rarely
 *
 * Ordered by frequency of use, because the top of a list is the cheapest place
 * to reach.
 */

interface NavItem {
  icon: LucideIcon;
  label: string;
  to: string;
  /** Shown in the collapsed tooltip to explain a non-obvious destination. */
  hint?: string;
}

interface NavSection {
  label: string;
  items: NavItem[];
}

const sections: NavSection[] = [
  {
    label: 'Work',
    items: [
      { icon: LayoutGrid, label: 'Dashboard', to: '/', hint: 'Overview of this project' },
      { icon: ClipboardList, label: 'My Inbox', to: '/inbox', hint: 'Tasks assigned to you' },
      { icon: Users, label: 'All Tasks', to: '/tasks', hint: 'Every task in this project' },
    ],
  },
  {
    label: 'Build',
    items: [
      { icon: Network, label: 'Processes', to: '/models', hint: 'Design and deploy process models' },
      { icon: Table2, label: 'Decisions', to: '/decisions', hint: 'Business rules as decision tables' },
      { icon: Zap, label: 'Connectors', to: '/connectors', hint: 'Connect to other systems' },
    ],
  },
  {
    label: 'Operate',
    items: [
      { icon: Play, label: 'Instances', to: '/instances', hint: 'Running and completed processes' },
    ],
  },
  {
    label: 'Administer',
    items: [
      { icon: FolderGit2, label: 'Projects', to: '/projects' },
      { icon: Building2, label: 'Organizations', to: '/organizations' },
      { icon: ShieldCheck, label: 'Groups', to: '/groups' },
      { icon: Users, label: 'People', to: '/users' },
    ],
  },
];

function NavLink({ item, collapsed }: { item: NavItem; collapsed: boolean }) {
  const link = (
    <Link
      to={item.to}
      className={classes.link}
      activeProps={{ 'data-active': true, 'aria-current': 'page' }}
      // Collapsed, the label is not rendered, so the icon alone would announce
      // as an unlabelled link. The accessible name comes from aria-label.
      aria-label={collapsed ? item.label : undefined}
    >
      <item.icon className={classes.linkIcon} size={19} strokeWidth={1.75} aria-hidden />
      {!collapsed && <span className={classes.linkLabel}>{item.label}</span>}
    </Link>
  );

  // A tooltip is the only way to identify an icon in the collapsed rail, and
  // the hint is useful even when expanded for destinations whose name does not
  // fully explain them.
  if (collapsed) {
    return (
      <Tooltip label={item.hint ? `${item.label} — ${item.hint}` : item.label} position="right" withArrow openDelay={200}>
        {link}
      </Tooltip>
    );
  }
  if (item.hint) {
    return (
      <Tooltip label={item.hint} position="right" withArrow openDelay={600}>
        {link}
      </Tooltip>
    );
  }
  return link;
}

export function Sidebar() {
  const { sidebarExpanded, toggleSidebar } = useAppStore();
  const collapsed = !sidebarExpanded;

  return (
    <nav
      className={`${classes.navbar} ${collapsed ? classes.collapsed : ''}`}
      aria-label="Main navigation"
    >
      <Link to="/" className={classes.brand} aria-label="Hermod BPM home">
        <span className={classes.brandMark} aria-hidden>
          <Network size={17} strokeWidth={2} />
        </span>
        {!collapsed && <span className={classes.brandName}>Hermod BPM</span>}
      </Link>

      <div className={classes.scroll}>
        {sections.map((section) => (
          <div key={section.label}>
            {collapsed ? (
              <div className={classes.sectionRule} aria-hidden />
            ) : (
              <div className={classes.sectionLabel}>{section.label}</div>
            )}
            {section.items.map((item) => (
              <NavLink key={item.to} item={item} collapsed={collapsed} />
            ))}
          </div>
        ))}
      </div>

      <div className={classes.footer}>
        {/*
          Theme, settings and logout used to live here as pseudo-nav links —
          "Settings" had no destination at all, and logout was duplicated in the
          header user menu. Account concerns belong in one place: the user menu.
        */}
        <button
          type="button"
          className={classes.collapseToggle}
          onClick={toggleSidebar}
          aria-label={collapsed ? 'Expand navigation' : 'Collapse navigation'}
          aria-expanded={sidebarExpanded}
        >
          {collapsed ? <ChevronsRight size={16} /> : <ChevronsLeft size={16} />}
          {!collapsed && <span>Collapse</span>}
        </button>
      </div>
    </nav>
  );
}
