import { Tooltip } from '@mantine/core';
import { Link } from '@tanstack/react-router';
import {
  Building2,
  Contact,
  ChevronsLeft,
  ChevronsRight,
  ClipboardList,
  FolderGit2,
  LayoutGrid,
  Network,
  Play,
  ShieldCheck,
  Table2,
  TerminalSquare,
  Users,
  Zap,
  type LucideIcon,
} from 'lucide-react';
import { useAppStore } from '../../store/useAppStore';
import classes from './Sidebar.module.css';
import { useTranslation } from '../../i18n/context';

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
  /** A message key, not a sentence — see src/i18n. */
  label: string;
  to: string;
  /** Shown in the collapsed tooltip to explain a non-obvious destination. */
  hint?: string;
}

interface NavSection {
  /** A message key. */
  label: string;
  items: NavItem[];
}

const sections: NavSection[] = [
  {
    label: 'nav.sectionWork',
    items: [
      { icon: LayoutGrid, label: 'nav.dashboard', to: '/', hint: 'Overview of this project' },
      { icon: ClipboardList, label: 'nav.inbox', to: '/inbox', hint: 'Tasks assigned to you' },
      { icon: Users, label: 'nav.allTasks', to: '/tasks', hint: 'Every task in this project' },
      // The people processes assign work to. Under Work rather than Administer
      // on purpose: they are a different population from the accounts that run
      // the platform, and putting the two side by side is how they came to be
      // one list in the first place.
      { icon: Contact, label: 'nav.people', to: '/people', hint: 'Who this project can assign work to' },
    ],
  },
  {
    label: 'nav.sectionBuild',
    items: [
      { icon: Network, label: 'nav.processes', to: '/models', hint: 'Design and deploy process models' },
      { icon: Table2, label: 'nav.decisions', to: '/decisions', hint: 'Business rules as decision tables' },
      { icon: Zap, label: 'nav.connectors', to: '/connectors', hint: 'Connect to other systems' },
      // Under Build rather than a developer section of its own: writing the
      // worker for a topic is part of building the process, and separating the
      // two is how a model ships with nothing on the other end of its topics.
      {
        icon: TerminalSquare,
        label: 'nav.sdkSandbox',
        to: '/sdk',
        hint: 'Try the API your integration will call',
      },
    ],
  },
  {
    label: 'nav.sectionOperate',
    items: [
      { icon: Play, label: 'nav.instances', to: '/instances', hint: 'Running and completed processes' },
    ],
  },
  {
    label: 'nav.sectionAdminister',
    items: [
      { icon: FolderGit2, label: 'nav.projects', to: '/projects' },
      { icon: Building2, label: 'nav.organizations', to: '/organizations' },
      { icon: ShieldCheck, label: 'nav.groups', to: '/groups' },
      { icon: Users, label: 'nav.platformAccess', to: '/users', hint: 'Accounts that administer Metis' },
    ],
  },
];

function NavLink({ item, collapsed }: { item: NavItem; collapsed: boolean }) {
  const { t } = useTranslation();
  const label = t(item.label);
  const link = (
    <Link
      to={item.to}
      className={classes.link}
      activeProps={{ 'data-active': true, 'aria-current': 'page' }}
      // Collapsed, the label is not rendered, so the icon alone would announce
      // as an unlabelled link. The accessible name comes from aria-label.
      aria-label={collapsed ? label : undefined}
    >
      <item.icon className={classes.linkIcon} size={19} strokeWidth={1.75} aria-hidden />
      {!collapsed && <span className={classes.linkLabel}>{label}</span>}
    </Link>
  );

  // A tooltip is the only way to identify an icon in the collapsed rail, and
  // the hint is useful even when expanded for destinations whose name does not
  // fully explain them.
  if (collapsed) {
    return (
      <Tooltip label={item.hint ? `${label} — ${item.hint}` : label} position="right" withArrow openDelay={200}>
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
  const { t } = useTranslation();
  const collapsed = !sidebarExpanded;

  return (
    // Not <nav>: AppShell.Navbar is already the navigation landmark, and a
    // nested one left two with the same name, which a screen reader offers as
    // two indistinguishable choices. The name lives on the outer element.
    <div className={`${classes.navbar} ${collapsed ? classes.collapsed : ''}`}>
      <Link to="/" className={classes.brand} aria-label="Metis BPM home">
        <span className={classes.brandMark} aria-hidden>
          <Network size={17} strokeWidth={2} />
        </span>
        {!collapsed && <span className={classes.brandName}>Metis BPM</span>}
      </Link>

      <div className={classes.scroll}>
        {sections.map((section) => (
          <div key={section.label}>
            {collapsed ? (
              <div className={classes.sectionRule} aria-hidden />
            ) : (
              <div className={classes.sectionLabel}>{t(section.label)}</div>
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
          {!collapsed && <span>{t('nav.collapse')}</span>}
        </button>
      </div>
    </div>
  );
}
