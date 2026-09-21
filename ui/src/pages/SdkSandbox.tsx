/**
 * The SDK Sandbox: drive a real process the way your code will.
 *
 * Integrating with a BPM engine fails in a particular way. The model is right,
 * the code compiles, and nothing happens — because a topic string has a capital
 * letter in it, or a message went out without the correlation key, or the step
 * is published for a worker that nobody has written yet. None of those report
 * themselves. Each one costs an afternoon.
 *
 * So this screen makes every one of those calls from the browser, against the
 * same public REST API the Go SDK speaks, and shows the exchange. Start an
 * instance, pull its work as a worker, hand a result back, tell it something
 * happened, and watch it move — then take the generated code away, knowing the
 * ids and names in it are the ones that just worked.
 *
 * Composition only. The run lives in `useSdkSandbox`, the wire contract in
 * `domain/sdkCalls`, and each step draws itself.
 */
import { Grid, Group, Paper, Tabs, Text, Badge, Button, Anchor } from '@mantine/core';
import { useState } from 'react';
import { ExternalLink, RotateCcw, TerminalSquare } from 'lucide-react';

import { PageHeader, PageShell } from '../components/layout';
import { EmptyState } from '../components/state';
import {
  SdkChooseStep,
  SdkCodePanel,
  SdkNotifyStep,
  SdkStartStep,
  SdkStep,
  SdkWatchStep,
  SdkWireLogPanel,
  SdkWorkStep,
} from '../components/sdk';
import classes from '../components/sdk/SdkStep.module.css';
import { defaultFocus, previewCall, stepStates, type Focus } from '../domain/sdkRun';
import type { SurfacePoint } from '../domain/sdkSurface';
import type { SnippetLanguage } from '../domain/sdkSnippets';
import { useSandboxProcesses } from '../hooks/useSandboxProcesses';
import { useSdkSandbox } from '../hooks/useSdkSandbox';
import { snippetBaseUrl } from '../services/domains/sdkSandboxService';
import { useAppStore } from '../store/useAppStore';

export function SdkSandbox() {
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  const sandbox = useSdkSandbox();
  const { form, patch } = sandbox;

  const [language, setLanguage] = useState<SnippetLanguage>('go');
  const [focus, setFocus] = useState<Focus | null>(null);
  const [panel, setPanel] = useState<string | null>('code');

  const { choices, loadingChoices, surface, loadingSurface } = useSandboxProcesses(
    form.definitionKey,
    form.version,
  );

  const states = stepStates(sandbox.instanceId, form.definitionKey);

  if (currentProjectId === null) {
    return (
      <PageShell>
        <PageHeader title="SDK Sandbox" description="Drive a real process the way your code will." />
        <EmptyState
          icon={TerminalSquare}
          title="Choose a project first"
          description="A sandbox run starts an instance in a project, so pick one from the header and come back."
        />
      </PageShell>
    );
  }

  const call = previewCall(
    focus ??
      defaultFocus({
        started: sandbox.instanceId !== null,
        holdingTask: sandbox.lockedTask !== null,
        hasTopics: (surface?.topics.length ?? 0) > 0,
      }),
    currentProjectId,
    form,
    sandbox.lockedTask,
  );

  return (
    <PageShell>
      <PageHeader
        title="SDK Sandbox"
        description="Make the calls your integration will make — against this server, with your ids — and take the code away."
        meta={
          sandbox.instanceId !== null ? (
            <Badge variant="light" color="teal" radius="sm">
              Run in progress
            </Badge>
          ) : undefined
        }
        actions={
          <Group gap="xs" wrap="nowrap">
            <Anchor
              href="https://github.com/gsoultan/metis-sdk"
              target="_blank"
              rel="noreferrer noopener"
              size="sm"
            >
              <Group gap={4} wrap="nowrap">
                <span>Go SDK</span>
                <ExternalLink size={13} aria-hidden />
              </Group>
            </Anchor>
            <Button
              variant="default"
              leftSection={<RotateCcw size={15} />}
              onClick={sandbox.reset}
              disabled={sandbox.instanceId === null && sandbox.log.length === 0}
            >
              Start over
            </Button>
          </Group>
        }
      />

      <Grid gap="xl">
        <Grid.Col span={{ base: 12, lg: 7 }}>
          <div onFocusCapture={() => setFocus(null)}>
            <SdkStep
              number={1}
              title="Pick a process"
              description="The model your code is going to drive."
              state={states.choose}
            >
              <SdkChooseStep
                choices={choices}
                loading={loadingChoices}
                definitionKey={form.definitionKey}
                version={form.version}
                onPickProcess={(key) => patch({ definitionKey: key, version: 0, topic: '' })}
                onPickVersion={(version) => patch({ version })}
                surface={surface}
                surfaceLoading={loadingSurface}
                selectedTopic={form.topic}
                selectedNotify={form.notifyName}
                onUseTopic={(topic) => {
                  patch({ topic });
                  setFocus('work');
                }}
                onUseNotify={(point: SurfacePoint) => {
                  patch({
                    notifyKind: point.correlationKey === undefined ? 'signal' : 'message',
                    notifyName: point.name,
                  });
                  setFocus('notify');
                }}
              />
            </SdkStep>
          </div>

          <div onFocusCapture={() => setFocus('start')}>
            <SdkStep
              number={2}
              title="Start an instance"
              description="The first call any integration makes."
              state={states.start}
            >
              <SdkStartStep
                definitionKey={form.definitionKey}
                variables={form.startVariables}
                variablesError={sandbox.variableErrors.start}
                idempotencyKey={form.idempotencyKey}
                instanceId={sandbox.instanceId}
                starting={sandbox.pending === 'start'}
                onVariablesChange={(startVariables) => patch({ startVariables })}
                onIdempotencyKeyChange={(idempotencyKey) => patch({ idempotencyKey })}
                onStart={() => void sandbox.startInstance(currentProjectId)}
              />
            </SdkStep>
          </div>

          <div onFocusCapture={() => setFocus('work')}>
            <SdkStep
              number={3}
              title="Do a step as your worker"
              description="Pull the work the engine published, and report back."
              state={states.work}
              meta={
                sandbox.lockedTask !== null ? (
                  <Badge variant="light" color="blue" radius="sm">
                    Holding a task
                  </Badge>
                ) : undefined
              }
            >
              <SdkWorkStep
                topics={surface?.topics ?? []}
                topic={form.topic}
                workerId={form.workerId}
                maxTasks={form.maxTasks}
                lockDurationMs={form.lockDurationMs}
                lockedTask={sandbox.lockedTask}
                noWorkOn={sandbox.noWorkOn}
                completeVariables={form.completeVariables}
                completeVariablesError={sandbox.variableErrors.complete}
                failMessage={form.failMessage}
                retries={form.retries}
                retryTimeoutMs={form.retryTimeoutMs}
                pending={sandbox.pending}
                hasInstance={sandbox.instanceId !== null}
                onChange={patch}
                onFetch={() => void sandbox.fetchAndLock()}
                onComplete={() => void sandbox.completeTask()}
                onFail={() => void sandbox.failTask()}
              />
            </SdkStep>
          </div>

          <div onFocusCapture={() => setFocus('notify')}>
            <SdkStep
              number={4}
              title="Tell it something happened"
              description="A message wakes one instance; a signal wakes every instance listening."
              state={states.notify}
            >
              <SdkNotifyStep
                inbound={surface?.inbound ?? []}
                kind={form.notifyKind}
                name={form.notifyName}
                correlationKey={form.correlationKey}
                variables={form.notifyVariables}
                variablesError={sandbox.variableErrors.notify}
                pending={sandbox.pending}
                onChange={patch}
                onSend={() => void sandbox.notify(currentProjectId)}
              />
            </SdkStep>
          </div>

          <div onFocusCapture={() => setFocus('watch')}>
            <SdkStep
              number={5}
              title="Watch what happened"
              description="The same status and timeline an operator sees."
              state={states.watch}
              connected={false}
            >
              <SdkWatchStep instanceId={sandbox.instanceId} />
            </SdkStep>
          </div>
        </Grid.Col>

        <Grid.Col span={{ base: 12, lg: 5 }}>
          <Paper withBorder radius="md" p="md" className={classes.panel}>
            <Tabs value={panel} onChange={setPanel} variant="pills" radius="sm">
              <Tabs.List mb="md" grow>
                <Tabs.Tab value="code">Your code</Tabs.Tab>
                <Tabs.Tab value="wire">
                  <Group gap={6} wrap="nowrap">
                    <span>Wire log</span>
                    {sandbox.log.length > 0 && (
                      <Badge size="xs" circle variant="filled" color="gray">
                        {sandbox.log.length}
                      </Badge>
                    )}
                  </Group>
                </Tabs.Tab>
              </Tabs.List>

              <Tabs.Panel value="code">
                <SdkCodePanel
                  call={call}
                  language={language}
                  onLanguageChange={setLanguage}
                  baseUrl={snippetBaseUrl()}
                />
              </Tabs.Panel>

              <Tabs.Panel value="wire">
                <SdkWireLogPanel log={sandbox.log} onClear={sandbox.clearLog} />
              </Tabs.Panel>
            </Tabs>
          </Paper>

          <Text size="xs" c="dimmed" mt="sm">
            Every call on this page goes through the public REST API, as your integration will —
            signed in as you, in this project.
          </Text>
        </Grid.Col>
      </Grid>
    </PageShell>
  );
}
