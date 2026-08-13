/**
 * The node type → component map React Flow renders with.
 *
 * It lives apart from the components because a module that exports both
 * components and a plain value cannot be hot-reloaded: editing any node would
 * reload the whole designer and lose the diagram being drawn.
 */
import {
  BoundaryNode,
  EndNode,
  GatewayNode,
  IntermediateNode,
  LaneNode,
  PoolNode,
  StartNode,
  SubProcessNode,
  TaskNode,
} from './BPMNNodes';

export const nodeTypes = {
  startEvent: StartNode,
  endEvent: EndNode,
  terminateEndEvent: EndNode,
  errorEndEvent: EndNode,
  intermediateCatchEvent: IntermediateNode,
  intermediateThrowEvent: IntermediateNode,
  timerEvent: IntermediateNode,
  boundaryEvent: BoundaryNode,
  compensationEvent: BoundaryNode,
  userTask: TaskNode,
  serviceTask: TaskNode,
  scriptTask: TaskNode,
  manualTask: TaskNode,
  businessRuleTask: TaskNode,
  callActivity: TaskNode,
  subProcess: SubProcessNode,
  exclusiveGateway: GatewayNode,
  parallelGateway: GatewayNode,
  inclusiveGateway: GatewayNode,
  eventBasedGateway: GatewayNode,
  pool: PoolNode,
  lane: LaneNode,
};
