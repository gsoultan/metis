import { Alert, Stack, Switch, Text, Textarea } from '@mantine/core';

import type { NodeConfigProps } from '../PropertyPanel';
import type { BPMNNodeData } from '../../types/bpmn';
import { asText } from '../../types/bpmn';
import { adHocToggle, adHocWarnings } from '../../domain/adHocSubProcess';
import { LoopSettings } from './LoopSettings';
import { PropertySection } from './PropertySection';

/**
 * A group of steps, run either in the order the diagram draws or in whatever
 * order the work turns out to need.
 *
 * The second kind is an ad-hoc sub-process, and until now there was no way to
 * make one here: the engine has executed them for a while, but the only way to
 * get one was to import a file that already had one. The decision about whether
 * a configuration will actually work lives in domain/adHocSubProcess so it can
 * be tested without rendering this.
 */
export function SubProcessConfig({ data, onUpdate, nodes = [], nodeId }: NodeConfigProps) {
  const isAdHoc = Boolean(data.isAdHoc);

  // Steps drawn inside this sub-process. The designer keeps children as a flat
  // list with a parent id rather than nesting them, so this counts rather than
  // reading a nested array that is empty on the canvas.
  const stepCount = nodeId
    ? nodes.filter((n) => asText(n.data?.parentId) === nodeId).length
    : 0;

  const warnings = adHocWarnings(
    {
      isAdHoc,
      completionCondition: asText(data.completionCondition),
      isEventSubProcess: Boolean(data.isEventSubProcess),
    },
    stepCount,
  );
  const warningFor = (field: string) => warnings.find((w) => w.field === field);

  return (
    <Stack gap="xl">
      <PropertySection
        title="How the steps inside run"
        hint="Most groups run in the order they are drawn. Turn this on for work where the order is not known in advance."
      >
        <Switch
          label="Let a person choose which steps to run, and in what order"
          description="Investigation, triage, case work — anything where the next step depends on what the last one found."
          checked={isAdHoc}
          // adHocToggle is a plain domain value; the panel's update shape is the
          // React Flow node data, which carries an index signature the domain
          // type deliberately does not.
          onChange={(e) => onUpdate(adHocToggle(e.currentTarget.checked) as Partial<BPMNNodeData>)}
        />
        {warningFor('steps') && (
          <Alert variant="light" color="red" p="sm" mt="sm">
            <Text size="xs">{warningFor('steps')?.message}</Text>
          </Alert>
        )}
      </PropertySection>

      {isAdHoc && (
        <PropertySection
          title="When this is finished"
          hint="Checked each time a step inside finishes. While it is false, the process waits here."
        >
          <Textarea
            label="Finished when"
            placeholder="e.g. checksDone >= 2"
            autosize
            minRows={2}
            value={asText(data.completionCondition)}
            onChange={(e) => onUpdate({ completionCondition: e.target.value })}
          />
          {warningFor('completionCondition') && (
            <Alert variant="light" color="yellow" p="sm" mt="sm">
              <Text size="xs">{warningFor('completionCondition')?.message}</Text>
            </Alert>
          )}
        </PropertySection>
      )}

      <PropertySection
        title="Started by an event"
        hint="An event sub-process runs when something happens elsewhere in the process, rather than when an arrow reaches it."
      >
        <Switch
          label="This group is started by an event"
          checked={Boolean(data.isEventSubProcess)}
          onChange={(e) => onUpdate({ isEventSubProcess: e.currentTarget.checked })}
        />
        {warningFor('isEventSubProcess') && (
          <Alert variant="light" color="red" p="sm" mt="sm">
            <Text size="xs">{warningFor('isEventSubProcess')?.message}</Text>
          </Alert>
        )}
      </PropertySection>

      {isAdHoc && stepCount > 0 && (
        <Alert variant="light" color="gray" p="sm">
          <Text size="xs">
            While the process is here, {stepCount === 1 ? 'the step' : `each of the ${stepCount} steps`} inside
            can be started from the running process, in any order and as many times as the work needs.
          </Text>
        </Alert>
      )}

      <LoopSettings data={data} onUpdate={onUpdate} />
    </Stack>
  );
}
