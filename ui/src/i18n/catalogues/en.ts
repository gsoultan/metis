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
  'nav.sdkSandbox': 'SDK Sandbox',
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

  // Platform access: who holds which role, and what each role is required
  // for. The role names and their one-line sentences come from domain/roles.ts
  // and are still English; the action names come from the server, which reads
  // them from its own gates, and are English too.
  'access.tabAccounts': 'Accounts',
  'access.tabRoles': 'Roles',
  'access.search': 'Search people',
  'access.searchPlaceholder': 'Search by name, username or email…',
  'access.columnPerson': 'Person',
  'access.legendButton': 'What {role} allows',
  'access.requiredFor': 'Required for',
  'access.requiredForNothing': 'No action is gated on this role alone.',
  'access.everythingElse': 'Anything not listed is open to anybody signed in.',
  'access.legendUnavailable': 'Could not load what this role allows. Try again later.',
  'access.holds': '{name} holds {role}',
  'access.lacks': '{name} does not hold {role}',
  'access.editHint': 'Tick a box to grant a role and clear it to take the role away. Each change is saved as you make it.',
  'access.readOnlyHint': 'Only an administrator can change who holds a role.',
  'access.cell': '{role} for {name}',
  'access.saving': 'Saving {role} for {name}…',
  'access.savedTitle': 'Roles changed',
  'access.granted': '{name} now holds {role}.',
  'access.revoked': '{name} no longer holds {role}.',
  'access.refusedTitle': 'Could not change {name}’s roles',
  'access.confirmOwnAdmin':
    'Take away your own Administrator role? You will no longer be able to manage accounts, and only another administrator can give it back.',
  'access.showing': '{count, plural, =1 {Showing the only account} other {Showing all # accounts}}',
  'access.matching': '{count} of {total} accounts match',
  'access.noMatch': 'Nobody matches “{query}”.',
  'access.empty': 'No accounts in this organization yet.',
  'access.area.processes': 'Processes',
  'access.area.decisions': 'Decisions',
  'access.area.connectors': 'Connectors',
  'access.area.webhooks': 'Webhooks',
  'access.area.instances': 'Running instances',
  'access.area.people': 'People',
  'access.area.directories': 'Directories',
  'access.area.projects': 'Projects',
  'access.area.organizations': 'Organizations',
  'access.area.groups': 'Groups',
  'access.area.accounts': 'Accounts',
  'access.area.platform': 'Platform accounts',
  'access.area.environments': 'Environments',
  'access.area.other': 'Other',

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

  // Getting started: the card on the Dashboard and the checklist in Help.
  'start.title': 'Getting started',
  'start.progress': '{done} of {total} done',
  'start.progressLabel': '{done} of {total} getting started steps done',
  'start.hide': 'Hide getting started',
  'start.hideHint': 'Your progress stays under Help, the question mark at the top.',
  'start.next': 'Next',
  'start.done': 'Done:',
  'start.unknownTitle': "Could not check this project's progress",
  'start.unknownHint': 'Nothing is shown as done or not done until it can be. Trying again often resolves it.',
  'start.deployProcess.label': 'Deploy a process',
  'start.deployProcess.description':
    'Draw one under Processes, or pick a template on the Dashboard, then deploy it so it can run.',
  'start.startInstance.label': 'Start an instance',
  'start.startInstance.description': 'Run your process once. Each run is an instance you can follow step by step.',
  'start.completeTask.label': 'Complete a task',
  'start.completeTask.description':
    'When a process needs a person, the task waits in the inbox until somebody completes it.',
  'start.connectSystem.label': 'Connect another system',
  'start.connectSystem.description': 'Set up a connection, such as email or Slack, so your steps can call it.',
  'start.addPeople.label': 'Add the people who do the work',
  'start.addPeople.description': 'Import the people your processes can assign tasks to.',

  // Help
  'help.title': 'Help',
  'help.glossary': 'Glossary',

  // The glossary. A step keeps the name the palette gives it, which is still
  // English, so that it can be looked up by the name somebody saw on it.
  'glossary.search': 'Search the glossary',
  'glossary.searchPlaceholder': 'Search, for example gateway or live version',
  'glossary.count': '{count, plural, one {# term} other {# terms}}',
  'glossary.matches': '{count} of {total} match',
  'glossary.noMatch': 'Nothing matches. Try a shorter word, or the other name for it.',
  'glossary.alsoCalled': 'also called {name}',
  'glossary.example': 'For example: {example}',
  'glossary.instance.term': 'Instance',
  'glossary.instance.definition':
    'One run of a process, from its start to its finish. Each one carries its own information and is at its own step.',
  'glossary.instance.example': 'Every expense claim somebody submits is its own instance of the expense process.',
  'glossary.deploy.term': 'Deploy',
  'glossary.deploy.definition':
    'Publishing a process so it can run. Each deploy saves a new version, and instances already running are left as they are.',
  'glossary.version.term': 'Version',
  'glossary.version.definition':
    'A numbered copy of a process, saved each time it is deployed. An instance stays on the version it started on unless somebody migrates it.',
  'glossary.liveVersion.term': 'Live version',
  'glossary.liveVersion.definition':
    'The version new instances start on. A process has one at a time: deploying normally makes the new version live, and Version history can make a different one live, straight away or at a time you choose.',
  'glossary.stagedVersion.term': 'Staged version',
  'glossary.stagedVersion.definition':
    'A version that is deployed but not live, so new instances keep starting on the live one. In Version history you can run it to try it, without making it live, then make it live with “Make live” or schedule it to take over at a time you choose.',
  'glossary.incident.term': 'Incident',
  'glossary.incident.definition':
    'A step that could not finish, so its instance waits there until somebody deals with it. Usually a call to another system that kept failing. A choice with no path to take raises one when an automatic step leads to it; when a person’s task leads to it, completing the task is refused instead, and the task stays open. Fix the cause, then retry the step.',
  'glossary.connection.term': 'Connection',
  'glossary.connection.definition':
    'A connector set up for one project, with that project’s own address and credentials: your Slack workspace, rather than Slack in general. A step that uses a connector calls through its project’s connection.',
  'glossary.connector.term': 'Connector',
  'glossary.connector.definition':
    'A ready-made way to call a kind of system, such as Slack, email, a database or a web API. A project sets up a connection to it before its steps can use it.',
  'glossary.decisionTable.term': 'Decision table',
  'glossary.decisionTable.definition':
    'Rules written as the lines of a table: when the inputs match a line, that line gives the answer. The policy lives in the table, so it can change without changing the process.',
  'glossary.decisionTable.example': 'Claims under £500 are approved automatically; larger ones go to a manager.',
  'glossary.hitPolicy.term': 'Hit policy',
  'glossary.hitPolicy.definition':
    'What a decision table does when more than one line matches: take the first, allow only one, collect every match, and so on.',
  'glossary.hitPolicy.example': 'Two discount lines match the same order, and the first line that matches wins.',
};

export default en;
