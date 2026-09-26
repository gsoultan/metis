import {
  Alert,
  Badge,
  Button,
  Group,
  Modal,
  Radio,
  Stack,
  Text,
} from "@mantine/core";
import { CircleDot, Info, Rocket, Save } from "lucide-react";
import { useState } from "react";

import { rolloutOutcome, type DeployMode, type VersionStatus } from "../domain/versionRollout";

interface DeployVersionModalProps {
  opened: boolean;
  onClose: () => void;
  processName: string;
  versions: VersionStatus[];
  deploying: boolean;
  /** Deploys, staged or live. */
  onDeploy: (mode: DeployMode) => void;
}

/**
 * Asks what a new version should do to work that is already running.
 *
 * The dialog exists because the honest answer used to be invisible. Deploying
 * promoted the new version in the same act that saved it, and the fate of
 * everything already in flight — it keeps running the old version, to
 * completion, always — was never stated anywhere. People reasonably assumed
 * either that their running approvals had jumped to the new model, or that the
 * deploy had broken them.
 *
 * So both options here spell out the same guarantee rather than hiding it: the
 * instances on the current version finish on the current version. The only thing
 * being chosen is where the *next* instance starts.
 */
export function DeployVersionModal({
  opened,
  onClose,
  processName,
  versions,
  deploying,
  onDeploy,
}: DeployVersionModalProps) {
  const outcome = rolloutOutcome(versions);
  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="xs">
          <Rocket size={20} />
          <Text fw={800}>
            Deploy {processName} v{outcome.version}
          </Text>
        </Group>
      }
      size="lg"
      radius="lg"
    >
      {/*
        The body is a separate component mounted only while the dialog is open,
        so its "stage" answer resets by unmounting. Reopening must not inherit
        the last answer: staging is a deliberate choice, and a remembered one
        would quietly stage a deploy somebody meant to put live.
      */}
      {opened && (
        <DeployVersionForm
          versions={versions}
          deploying={deploying}
          onClose={onClose}
          onDeploy={onDeploy}
        />
      )}
    </Modal>
  );
}

interface DeployVersionFormProps {
  versions: VersionStatus[];
  deploying: boolean;
  onClose: () => void;
  onDeploy: (mode: DeployMode) => void;
}

function DeployVersionForm({
  versions,
  deploying,
  onClose,
  onDeploy,
}: DeployVersionFormProps) {
  const [stage, setStage] = useState(false);
  const outcome = rolloutOutcome(versions);
  const draining = outcome.draining;
  const incumbent = outcome.incumbent;

  return (
    <Stack gap="md">
      <Radio.Group
        value={stage ? "stage" : "live"}
        onChange={(value) => setStage(value === "stage")}
      >
        <Stack gap="sm">
          <Radio.Card p="md" radius="md" value="live">
            <Group align="flex-start" gap="sm" wrap="nowrap">
              <Radio.Indicator />
              <Stack gap={4}>
                <Group gap="xs">
                  <Text fw={700} size="sm">
                    Make v{outcome.version} the live version
                  </Text>
                  <Badge size="xs" color="green" variant="light">
                    Recommended
                  </Badge>
                </Group>
                <Text size="xs" c="dimmed">
                  New instances start on v{outcome.version} from now on.
                  {incumbent !== null && draining > 0 && (
                    <>
                      {" "}
                      The{" "}
                      {draining === 1
                        ? "one instance"
                        : `${draining} instances`}{" "}
                      still running on v{incumbent}{" "}
                      {draining === 1 ? "finishes" : "finish"} on v{incumbent}.
                    </>
                  )}
                  {incumbent !== null && draining === 0 && (
                    <> Nothing is currently running on v{incumbent}.</>
                  )}
                </Text>
              </Stack>
            </Group>
          </Radio.Card>

          <Radio.Card p="md" radius="md" value="stage">
            <Group align="flex-start" gap="sm" wrap="nowrap">
              <Radio.Indicator />
              <Stack gap={4}>
                <Text fw={700} size="sm">
                  Stage v{outcome.version} for later
                </Text>
                <Text size="xs" c="dimmed">
                  v{outcome.version} is saved but takes no work.
                  {incumbent !== null && (
                    <>
                      {" "}
                      v{incumbent} stays live and keeps taking new instances.
                    </>
                  )}{" "}
                  Promote it from Processes → Version history when you are
                  ready.
                </Text>
              </Stack>
            </Group>
          </Radio.Card>
        </Stack>
      </Radio.Group>

      <Alert variant="light" color="blue" icon={<Info size={16} />} radius="md">
        <Text size="xs">
          Either way, instances already running are never moved to a different
          version. Each one finishes on the process model it started with — so a
          change to a step cannot alter work that is already part-way through
          it.
        </Text>
      </Alert>

      <Group justify="flex-end">
        <Button variant="default" onClick={onClose} disabled={deploying}>
          Cancel
        </Button>
        <Button
          color={stage ? "gray" : "indigo"}
          leftSection={stage ? <Save size={16} /> : <CircleDot size={16} />}
          loading={deploying}
          onClick={() => onDeploy(stage ? 'staged' : 'live')}
        >
          {stage
            ? `Stage v${outcome.version}`
            : `Deploy v${outcome.version} live`}
        </Button>
      </Group>
    </Stack>
  );
}
