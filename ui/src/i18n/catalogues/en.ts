/**
 * The English catalogue: the source of truth.
 *
 * Keys are `area.thing`, flat and sorted, so a translator can read the file top
 * to bottom and a reviewer can see what changed in a diff. The English text
 * here is the wording the product actually uses — when it changes, this is the
 * file that changes, and every other catalogue is then visibly behind.
 *
 * Only strings that have been migrated are here. The rest of the interface is
 * still hardcoded English; see src/i18n/README.md for the convention and what
 * remains.
 */
import type { Catalogue } from '../translate';

const en: Catalogue = {
  // Shell and navigation
  'nav.dashboard': 'Dashboard',
  'nav.inbox': 'My Inbox',
  'nav.allTasks': 'All Tasks',
  'nav.processes': 'Processes',
  'nav.decisions': 'Decisions',
  'nav.connectors': 'Connectors',
  'nav.instances': 'Instances',
  'nav.projects': 'Projects',
  'nav.organizations': 'Organizations',
  'nav.groups': 'Groups',
  'nav.people': 'People',
  'nav.platformAccess': 'Platform access',
  'nav.sectionWork': 'Work',
  'nav.sectionBuild': 'Build',
  'nav.sectionOperate': 'Operate',
  'nav.sectionAdminister': 'Administer',
  'nav.collapse': 'Collapse',
  'nav.mainLabel': 'Main navigation',

  // Signing in
  'login.welcome': 'Welcome back',
  'login.subtitle': 'Sign in to continue to your workspace',
  'login.username': 'Username',
  'login.password': 'Password',
  'login.submit': 'Sign in',
  'login.failedTitle': 'Could not sign in',
  'login.failedHelp': 'Check your username and password, or ask an administrator to reset it.',

  // The inbox
  'inbox.title': 'Task Inbox',
  'inbox.subtitle': 'Manage and complete tasks for {name}.',
  'inbox.assignedToMe': 'Assigned to Me',
  'inbox.availableToClaim': 'Available to Claim',
  'inbox.search': 'Search tasks…',
  'inbox.columnTask': 'Task Info',
  'inbox.columnAssignment': 'Assignment',
  'inbox.columnAbout': 'What it is about',
  'inbox.columnTimeline': 'Timeline',
  'inbox.columnStatus': 'Status',
  'inbox.columnActions': 'Actions',
  'inbox.claim': 'Claim Task',
  'inbox.complete': 'Complete',
  'inbox.noDueDate': 'No due date',
  'inbox.reference': 'Reference {code}',
  'inbox.allCaughtUp': "You're all caught up",
  'inbox.nothingWaiting':
    '{count, plural, =0 {Nothing needs your attention} one {# task waiting} other {# tasks waiting}}',

  // Offline and updates
  'offline.title': 'You are offline',
  'offline.body':
    'You can still read what has already loaded, and complete the tasks in your inbox. They will be sent when you are back online.',
  'offline.savedTitle': 'Saved on this device',
  'offline.queued':
    '{count, plural, one {# change waiting to be sent} other {# changes waiting to be sent}}',
  'offline.sending': 'Sending them now. They are kept here until the server confirms each one.',
  'offline.keptHere': 'Kept on this device until you are back online.',
  'offline.tryAgain': 'Try again now',
  'update.title': 'A new version is ready',
  'update.body':
    'Reload when you are at a good stopping point. Anything you are part way through typing is not saved yet.',
  'update.reload': 'Reload now',
  'update.later': 'Later',

  // Words used all over
  'common.cancel': 'Cancel',
  'common.save': 'Save',
  'common.delete': 'Delete',
  'common.close': 'Close',
  'common.retry': 'Try again',
  'common.loading': 'Loading…',
  'common.language': 'Language',

  // Page headings.
  //
  // Every page title and standfirst the user lands on. These were hardcoded
  // English in each page component, so switching the interface to Indonesian
  // translated the seventeen navigation labels and nothing else — one screen
  // where the sidebar read "Dasbor" and the heading beside it read "Dashboard".
  'page.dashboard.title': 'Dashboard',
  'page.dashboard.subtitle': 'Overview of your business processes and tasks.',
  'page.welcome.title': 'Welcome to Metis BPM',
  'page.welcome.subtitle': 'Get started by selecting or creating a project.',
  'page.allTasks.title': 'All Tasks',
  'page.allTasks.subtitle': 'Every task in this project, whoever it belongs to.',
  'page.instances.title': 'Process Instances',
  'page.instances.subtitle': 'Every run of a process in this project, and where each one is.',
  'page.processes.title': 'Processes',
  'page.processes.subtitle': 'Design, deploy and version the process models this project runs.',
  'page.decisions.title': 'Decisions',
  'page.decisions.subtitle': 'Business rules as decision tables, versioned and testable.',
  'page.decisionTables.title': 'Decision Tables',
  'page.decisionTables.subtitle': 'Manage your DMN-compatible decision tables and business rules.',
  'page.definitions.title': 'Processes',
  'page.definitions.subtitle': 'Design, deploy and manage your business process models.',
  'page.connectors.title': 'Connectors',
  'page.connectors.subtitle': 'The services your processes call, and the ones that call them.',
  'page.people.title': 'People',
  'page.people.subtitle': "Who this project's processes can assign work to. Separate from the accounts that administer Metis.",
  'page.groups.title': 'Groups',
  'page.groups.subtitle': 'Manage user groups and memberships.',
  'page.projects.title': 'Projects',
  'page.projects.subtitle': 'Organize your processes and tasks into projects.',
  'page.organizations.title': 'Organizations',
  'page.organizations.subtitle': 'Manage your organizations and their projects.',
  'page.platformAccess.title': 'Platform access',
  'page.platformAccess.subtitle': 'Accounts that administer Metis: they sign in, configure the installation and author models. The people processes assign work to are Participants, on the People page.',
  'page.profile.title': 'User Profile',
  'page.profile.subtitle': 'Manage your personal information and account settings.',
  'page.settings.title': 'Application Settings',
  'page.settings.subtitle': 'Configure your workspace and preferences.',

  // The dashboard, which is where the interface's own language was most
  // visibly at odds with itself: the navigation translated and the cards
  // beside it did not.
  'dash.activeInstances': 'Active Instances',
  'dash.activeInstancesHint': 'Processes currently running',
  'dash.processModels': 'Process Models',
  'dash.processModelsHint': 'Deployed definitions in this project',
  'dash.tasksCompleted': 'Tasks Completed',
  'dash.tasksProgress': '{done} of {total}',
  'dash.needsAttention': 'Needs Attention',
  'dash.needsAttentionSome': 'Instances stuck and waiting on someone',
  'dash.needsAttentionNone': 'Nothing is stuck',
  'dash.timeline': 'Business Timeline',
  'dash.recentActivity': 'Recent Activity',
  'dash.viewAllInstances': 'View all instances',
  'dash.generateReport': 'Generate Report',
  'dash.noActivity': 'No recent activity',
  'dash.noActivityHint': 'Start a process to see the activity timeline here.',
  'dash.readyTitle': 'Ready to automate?',
  'dash.readyLoading': 'Loading your project. If this stays here, pick one from the header.',
  'dash.readyNoProjects': "Projects group related process models, tasks and instances. You'll need one to start.",
  'dash.createFirstProject': 'Create your first project',
  'common.complete': 'Complete',
  'dash.startFrom': 'Start from a template',
  'dash.startFromHint': 'A working process you can edit, instead of an empty canvas',
  'dash.useTemplate': 'Use this template',
};

export default en;
