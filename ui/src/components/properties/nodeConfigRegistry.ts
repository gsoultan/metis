import type { ComponentType } from 'react';

import type { NodeConfigProps } from '../PropertyPanel';
import { BusinessRuleTaskConfig } from './BusinessRuleTaskConfig';
import { CallActivityConfig } from './CallActivityConfig';
import { EventConfig } from './EventConfig';
import { GatewayConfig } from './GatewayConfig';
import { ManualTaskConfig } from './ManualTaskConfig';
import { ScriptTaskConfig } from './ScriptTaskConfig';
import { ServiceTaskConfig } from './ServiceTaskConfig';
import { StartEventConfig } from './StartEventConfig';
import { SubProcessConfig } from './SubProcessConfig';
import { ThrowEventConfig } from './ThrowEventConfig';
import { UserTaskConfig } from './UserTaskConfig';

/** Node type string → property config component. */
export const CONFIG_REGISTRY: Record<string, ComponentType<NodeConfigProps>> = {
  userTask: UserTaskConfig,
  manualTask: ManualTaskConfig,
  businessRuleTask: BusinessRuleTaskConfig,
  callActivity: CallActivityConfig,
  serviceTask: ServiceTaskConfig,
  scriptTask: ScriptTaskConfig,
  intermediateCatchEvent: EventConfig,
  // The throwing events get their own panel: EventConfig asks what a step
  // *waits for*, which is the wrong question for one that announces something.
  intermediateThrowEvent: ThrowEventConfig,
  errorEndEvent: ThrowEventConfig,
  escalationThrowEvent: ThrowEventConfig,
  compensationThrowEvent: ThrowEventConfig,
  boundaryEvent: EventConfig,
  signalEvent: EventConfig,
  messageEvent: EventConfig,
  timerEvent: EventConfig,
  startEvent: StartEventConfig,
  exclusiveGateway: GatewayConfig,
  inclusiveGateway: GatewayConfig,
  eventBasedGateway: GatewayConfig,
  // A sub-process had no panel at all, so the one decision that changes how it
  // runs — whether its steps are driven by the diagram or by a person — could
  // only be made by importing a file that already said so.
  subProcess: SubProcessConfig,
};
