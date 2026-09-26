import { 
  Grid, 
  Card, 
  Text, 
  Group, 
  Stack, 
  ThemeIcon, 
  Title, 
  Button, 
  Badge, 
  Box,
  rem, 
  Progress,
} from '@mantine/core';
import type { LucideIcon } from 'lucide-react';
import { 
  GitBranch, 
  TrendingUp, 
  Activity, 
  CheckCircle, 
  AlertCircle,
  Printer,
} from 'lucide-react';
import { notifications } from '@mantine/notifications';
import { 
  useDefinitions, 
  useProjects,
  useProcessStatistics,
  useInstances,
  useWaitingByStep,
} from '../hooks/useProcess';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { BusinessTimeline } from '../components/BusinessTimeline';
import { GettingStartedCard } from '../components/GettingStartedCard';
import { useGettingStartedProgress } from '../hooks/useGettingStarted';
import { Link, useNavigate } from '@tanstack/react-router';
import { StatsLoadingState, ErrorState } from '../components/state';
import { PROCESS_TEMPLATES } from '../domain/processTemplates';
import { useTranslation } from '../i18n/context';
import { useMemo } from 'react';
import { useDeadlines } from '../hooks/useTasks';
import { csvBlob, csvFilename } from '../domain/csv';
import { slaReportCsv, slaReportFromDeadlines, slaSummary } from '../domain/slaReport';
import { taskCompletion } from '../domain/dashboardFigures';
import { heatColor, heatFromWaiting, heatmapCsv, heatSummary } from '../domain/processHeatmap';
import { errorMessage } from '../services/shared/errors';

/**
 * A single headline number.
 *
 * There is deliberately no `trend` prop. It used to accept a string, and every
 * call site passed a hardcoded one ("+12%", "+5%", "+2%") rendered beside a
 * green upward arrow and the words "vs last month" — while no endpoint in the
 * product computes a trend of any kind. Removing the prop means the fabrication
 * cannot come back without someone first building the data.
 *
 * `progress` is only for values that genuinely are a percentage of a whole.
 */
function StatCard({
  title,
  value,
  icon: Icon,
  color,
  progress,
  progressLabel,
  hint,
}: {
  title: string;
  value: React.ReactNode;
  icon: LucideIcon;
  color: string;
  progress?: number;
  progressLabel?: string;
  hint?: string;
}) {
  return (
    <Card shadow="md" radius="lg">
      <Group justify="space-between" align="flex-start" mb="sm">
        <Stack gap={0}>
          <Text size="xs" c="dimmed" fw={700} tt="uppercase" lts={rem(1)}>
            {title}
          </Text>
          <Text size="xl" fw={800} mt={5} style={{ fontSize: rem(28) }}>
            {value}
          </Text>
        </Stack>
        <ThemeIcon size={rem(48)} radius="md" variant="light" color={color}>
          <Icon size={rem(24)} />
        </ThemeIcon>
      </Group>
      
      {progress !== undefined && (
        <Stack gap={4} mt="md">
          <Group justify="space-between" align="flex-end">
            <Text size="xs" c="dimmed" fw={600}>{progressLabel ?? 'Complete'}</Text>
            <Text size="xs" fw={700} c={color}>{progress}%</Text>
          </Group>
          <Progress
            value={progress}
            color={color}
            size="sm"
            radius="xl"
            aria-label={`${progressLabel ?? 'Complete'}: ${progress}%`}
          />
        </Stack>
      )}

      {hint && (
        <Text size="xs" c="dimmed" mt="md">{hint}</Text>
      )}
    </Card>
  );
}

export function Dashboard() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { currentProjectId, currentOrganizationId } = useAppStore();
  const { data: statsData, isLoading: statsLoading, error: statsError, refetch: refetchStats } = useProcessStatistics();
  const { data: defs } = useDefinitions();
  const { data: projectsData } = useProjects(currentOrganizationId);
  const { data: instancesData } = useInstances();
  // The open work with a deadline, for the report below. The inbox already
  // tells one person that one task is late; nothing answered "how much is
  // late, and whose" for somebody who has to do something about it. Read on
  // the server across all of it: a page of the task list held the newest 200
  // tasks of any status, and named no process.
  const { data: deadlines } = useDeadlines();

  const report = useMemo(() => slaReportFromDeadlines(deadlines ?? {}), [deadlines]);

  // Where the running work is sitting. The instance list already says which
  // step each instance is on, one row at a time; this asks it the other way
  // round, which is the direction that finds a bottleneck. Counted on the
  // server across all of it; it used to be counted here from one page.
  const { data: waiting } = useWaitingByStep();
  const heat = useMemo(() => heatFromWaiting(waiting ?? []), [waiting]);

  // Where somebody new stands. Called with the rest, before the return below
  // for a missing project: a hook called only on some renders breaks React's
  // count of them. Its definitions, instances and statistics are the queries
  // above, answered from the same cache.
  const gettingStarted = useGettingStartedProgress();
  

  // Falling back to zeros made an unloaded dashboard indistinguishable from a
  // real, idle one — "we don't know yet" rendered as "we know, and it's none".
  // The zeros remain only as a shape for the render below; statsLoading decides
  // whether they are ever shown.
  /*
   * These come from the Connect (protobuf) client, which serialises field
   * names in camelCase — activeInstances, not active_instances.
   *
   * Every read here used snake_case, so each one evaluated to `undefined` and
   * the `|| 0` fallback rendered a zero. The dashboard showed 0 active
   * instances, 0 process models and 0% completion no matter what the system
   * was actually doing, and nothing caught it because the processService
   * facade was typed `any`.
   */
  const stats = statsData?.stats;
  const activeInstances = stats?.activeInstances ?? 0;

  /*
   * Deliberately NOT stats.failedInstances, which counts instances whose
   * *status* is FAILED. A BPMN instance whose job exhausts its retries does not
   * go FAILED — it stays RUNNING and raises an incident. So that counter read 0
   * while four instances sat stuck on a dead HTTP call, and this card told the
   * operator "Nothing has failed" on their first screen.
   *
   * `needsAttentionTotal` is what the Instances page already filters on, so the
   * dashboard and that page now answer the same question with the same number.
   */
  const needsAttention = instancesData?.needsAttentionTotal ?? 0;

  const lastInstanceId = instancesData?.instances?.[0]?.id;

  /*
   * Distinct process keys, not rows. `definitions` holds one row per deployed
   * *version*, so counting it said "8 process models" for the two processes the
   * Models page lists. Both numbers were defensible and a user reading them an
   * hour apart could not reconcile them.
   */
  const totalDefinitions = new Set((defs?.definitions ?? []).map((d) => d.key)).size;
  const totalProjects = projectsData?.projects?.length || 0;


  if (!currentProjectId) {
    return (
      <Stack gap="xl">
        <PageHeader 
          title={t('page.welcome.title')}
          description={t('page.welcome.subtitle')}
        />
        
        <Card shadow="sm" radius="lg" withBorder py={60}>
          <Stack align="center" gap="md">
            <ThemeIcon size={80} radius="xl" variant="light" color="indigo">
              <TrendingUp size={40} />
            </ThemeIcon>
            <Title order={2}>{t('dash.readyTitle')}</Title>
            {/*
              A project is now chosen automatically, so this is reached when
              there is none to choose rather than because somebody skipped a
              step. Telling them to "select one from the header" when the header
              is empty was the old, unhelpful half of this.
            */}
            <Text c="dimmed" ta="center" maw={500}>
              {totalProjects > 0
                ? t('dash.readyLoading')
                : t('dash.readyNoProjects')}
            </Text>

            {totalProjects === 0 && (
              <Button component={Link} to="/projects" size="md" radius="md" color="indigo">
                {t('dash.createFirstProject')}
              </Button>
            )}
          </Stack>
        </Card>
      </Stack>
    );
  }

  const completion = taskCompletion(stats ?? {});

  // The report prints what this screen has read, so it waits for all of it:
  // printed before the deadlines arrive, it would say nothing is late.
  const reportReady = !statsLoading && !statsError && deadlines !== undefined && waiting !== undefined;
  const printReport = async () => {
    try {
      const { printProcessReport } = await import('../components/report/printProcessReport');
      printProcessReport({
        projectName: projectsData?.projects?.find((project) => project.id === currentProjectId)?.name ?? 'Project',
        generatedAt: new Date(),
        activeInstances,
        processModels: totalDefinitions,
        completion,
        needsAttention,
        report,
        heat,
      });
    } catch (err) {
      notifications.show({
        title: 'Could not prepare the report',
        message: errorMessage(err),
        color: 'red',
      });
    }
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title={t('page.dashboard.title')}
        description={t('page.dashboard.subtitle')}
        actions={
          <Button variant="light" leftSection={<Printer size={16} />} disabled={!reportReady} onClick={printReport}>
            {t('dash.generateReport')}
          </Button>
        }
      />

      <GettingStartedCard progress={gettingStarted.progress} onRetry={gettingStarted.retry} />

      {statsLoading ? (
        <StatsLoadingState count={4} />
      ) : statsError ? (
        <ErrorState error={statsError} action="load your statistics" onRetry={() => refetchStats()} />
      ) : (
      <Grid gap="xl">
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.activeInstances')}
            value={activeInstances}
            icon={Activity}
            color="indigo"
            hint={t('dash.activeInstancesHint')}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.processModels')}
            value={totalDefinitions}
            icon={GitBranch}
            color="teal"
            hint={t('dash.processModelsHint')}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.tasksCompleted')}
            value={`${completion.rate}%`}
            icon={CheckCircle}
            color="orange"
            progress={completion.rate}
            progressLabel={t('dash.tasksProgress', { done: completion.done, total: completion.total })}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.needsAttention')}
            value={needsAttention}
            icon={AlertCircle}
            color={needsAttention > 0 ? 'red' : 'green'}
            hint={
              needsAttention > 0
                ? t('dash.needsAttentionSome')
                : t('dash.needsAttentionNone')
            }
          />
        </Grid.Col>
      </Grid>
      )}

      {/*
        Deadlines, for the reader the inbox does not serve. The inbox tells one
        person that one task of theirs is late; this is how much is late across
        the project and who is carrying it, which is the question asked at the
        end of a week rather than at the start of a task.
      */}
      <Card shadow="sm" radius="lg" withBorder mb="xl">
        <Group justify="space-between" mb="md">
          <Group gap="sm">
            <Title order={4}>Deadlines</Title>
            {report.breached.length > 0 && (
              <Badge variant="light" color="red" radius="sm">
                {report.breached.length} late
              </Badge>
            )}
          </Group>
          <Button
            variant="subtle"
            size="xs"
            disabled={report.breached.length === 0}
            onClick={() => downloadCsv(slaReportCsv(report), csvFilename('late-work'))}
          >
            Export CSV
          </Button>
        </Group>

        <Text size="sm" c={report.breached.length > 0 ? undefined : 'dimmed'} mb={report.breached.length > 0 ? 'md' : 0}>
          {slaSummary(report)}
        </Text>

        {report.breached.length > 0 && (
          <Grid>
            <Grid.Col span={{ base: 12, md: 6 }}>
              <Text size="xs" fw={600} mb={4}>Who is carrying it</Text>
              {report.byAssignee.slice(0, 5).map((group) => (
                <Group key={group.name} justify="space-between" wrap="nowrap">
                  <Text size="xs">{group.name}</Text>
                  <Text size="xs" c="dimmed">
                    {group.breached} late, worst {group.worstLateBy}
                  </Text>
                </Group>
              ))}
            </Grid.Col>
            <Grid.Col span={{ base: 12, md: 6 }}>
              <Text size="xs" fw={600} mb={4}>Which process</Text>
              {report.byProcess.slice(0, 5).map((group) => (
                <Group key={group.name} justify="space-between" wrap="nowrap">
                  <Text size="xs">{group.name}</Text>
                  <Text size="xs" c="dimmed">
                    {group.breached} late, worst {group.worstLateBy}
                  </Text>
                </Group>
              ))}
            </Grid.Col>
          </Grid>
        )}
      </Card>

      {/*
        Where the work is sitting. The instance list answers "where is this
        quotation" a row at a time; this asks which step is holding the most,
        which is the direction that finds a bottleneck.

        About now rather than about history on purpose: a count of how often a
        step has ever run flatters the busy ones and hides the one where forty
        quotations are stuck this morning.
      */}
      <Card shadow="sm" radius="lg" withBorder mb="xl">
        <Group justify="space-between" mb="md">
          <Title order={4}>Where work is waiting</Title>
          <Group gap="xs">
            <Button
              variant="subtle"
              size="xs"
              disabled={heat.length === 0}
              onClick={() => downloadCsv(heatmapCsv(heat), csvFilename('waiting-work'))}
            >
              Export CSV
            </Button>
            <Button component={Link} to="/instances" variant="subtle" size="xs">
              {t('dash.viewAllInstances')}
            </Button>
          </Group>
        </Group>

        <Text size="sm" c={heat.length > 0 ? undefined : 'dimmed'} mb={heat.length > 0 ? 'md' : 0}>
          {heatSummary(heat)}
        </Text>

        {heat.map((process) => (
          <Stack key={process.processName} gap={4} mb="md">
            <Text size="xs" fw={600}>{process.processName}</Text>
            {process.nodes.slice(0, 6).map((node) => (
              <Group key={node.nodeId} gap="xs" wrap="nowrap" align="center">
                <Text size="xs" style={{ minWidth: 160 }}>{node.label}</Text>
                {/*
                  The bar is the comparison; the number is the fact. Both,
                  because a bar alone cannot be read off and a number alone
                  cannot be scanned.
                */}
                <Progress
                  value={node.intensity * 100}
                  color={heatColor(node.intensity)}
                  size="sm"
                  radius="sm"
                  style={{ flex: 1 }}
                  aria-label={`${node.waiting} waiting on ${node.label}`}
                />
                <Text size="xs" c="dimmed" style={{ minWidth: 28 }} ta="right">
                  {node.waiting}
                </Text>
              </Group>
            ))}
          </Stack>
        ))}
      </Card>

      <Grid gap="xl">
        <Grid.Col span={12}>
          <Card shadow="sm" radius="lg" withBorder h="100%">
            <Group justify="space-between" mb="xl">
              <Group gap="sm">
                <Title order={4}>{t('dash.timeline')}</Title>
                <Badge variant="light" color="indigo" radius="sm">{t('dash.recentActivity')}</Badge>
              </Group>
              <Button component={Link} to="/instances" variant="subtle" size="xs">{t('dash.viewAllInstances')}</Button>
            </Group>
            
            {lastInstanceId ? (
              <BusinessTimeline instanceId={lastInstanceId} />
            ) : (
              <Stack align="center" py={60} gap="sm">
                <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                  <Activity size={32} />
                </ThemeIcon>
                <Text fw={700}>{t('dash.noActivity')}</Text>
                <Text size="sm" c="dimmed">{t('dash.noActivityHint')}</Text>
              </Stack>
            )}
          </Card>
        </Grid.Col>
        
      </Grid>


      {/*
        This section was removed in #57 and is back because the feature exists
        now. It previously offered three cards badged "Recommended" whose every
        button was disabled, which is prime dashboard space advertising
        something that could not be used.

        The templates are real diagrams — see domain/processTemplates.ts — and
        each card opens the designer with one already drawn.
      */}
      <Card shadow="sm" radius="lg" withBorder mb="xl">
        <Group justify="space-between" mb="lg">
          <Group gap="sm">
            <ThemeIcon color="indigo" variant="light" size="lg">
              <GitBranch size={20} />
            </ThemeIcon>
            <Title order={4}>{t('dash.startFrom')}</Title>
          </Group>
          <Text size="xs" c="dimmed">{t('dash.startFromHint')}</Text>
        </Group>

        <Grid gap="md">
          {PROCESS_TEMPLATES.map((template) => (
            <Grid.Col span={{ base: 12, md: 4 }} key={template.id}>
              <Card withBorder padding="md" radius="md" h="100%">
                <Stack gap="sm" h="100%" justify="space-between">
                  <Box>
                    <Text size="md" fw={700}>{template.name}</Text>
                    <Text size="xs" c="dimmed" mt={4}>{template.description}</Text>
                  </Box>
                  <Button
                    onClick={() => navigate({
                      to: '/designer',
                      search: { template: template.id, name: template.name, key: template.suggestedKey },
                    })}
                    variant="light"
                    color="indigo"
                    size="xs"
                    fullWidth
                  >
                    {t('dash.useTemplate')}
                  </Button>
                </Stack>
              </Card>
            </Grid.Col>
          ))}
        </Grid>
      </Card>

    </Stack>
  );
}


/**
 * Hands the browser a file.
 *
 * An object URL rather than a data: one because a data URL carrying a few
 * hundred late tasks runs into length limits in exactly the situation the
 * report matters most.
 */
function downloadCsv(contents: string, filename: string) {
  const blob = csvBlob(contents);
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}
