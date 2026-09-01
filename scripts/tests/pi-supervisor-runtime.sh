#!/bin/sh
set -eu

skip() { printf 'SKIP: %s\n' "$*"; exit 0; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
ROOT=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)

BOOTSTRAP_HOME=${TMPDIR:-/tmp}/pitcrew-pi-bootstrap.$$
mkdir -p "$BOOTSTRAP_HOME/npm/node_modules/pi-subagents" "$BOOTSTRAP_HOME/extensions/subagent"
printf '%s\n' '{"name":"pi-subagents","version":"0.25.0"}' > "$BOOTSTRAP_HOME/npm/node_modules/pi-subagents/package.json"
printf '%s\n' '{"packages":["npm:pi-subagents"]}' > "$BOOTSTRAP_HOME/settings.json"
printf '%s\n' '{"maxSubagentDepth":3}' > "$BOOTSTRAP_HOME/extensions/subagent/config.json"
PI_AGENT_HOME=$BOOTSTRAP_HOME sh "$ROOT/scripts/install-templates.sh" pi >/dev/null || fail 'could not render Pi bootstrap definitions'

roles='daimon aion pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer'
for role in $roles; do
  prompt=$BOOTSTRAP_HOME/agents/$role.md
  [ -f "$prompt" ] || fail "missing Pi role $role"
  case $role in
    daimon|aion|pc2-sdd-initializer) command="pitcrew agent brief --role $role" ;;
    pc2-implementer) command="pitcrew agent brief --role $role --workflow-id <received-workflow-id> --unit-id <received-unit-id>" ;;
    pc2-reviewer) command="pitcrew agent brief --role $role --workflow-id <received-workflow-id> [--unit-id <received-unit-id>]" ;;
    *) command="pitcrew agent brief --role $role --workflow-id <received-workflow-id>" ;;
  esac
  command_line=$(grep -nF "$command" "$prompt" | head -1 | cut -d: -f1)
  action_line=$(grep -nF 'before taking action' "$prompt" | head -1 | cut -d: -f1)
  [ -n "$command_line" ] && [ -n "$action_line" ] && [ "$command_line" -le "$action_line" ] || fail "$role bootstrap does not precede action"
  grep -F "Identity: You are the $role PitCrew agent." "$prompt" >/dev/null || fail "$role identity drifted"
  case $command in *'<received-'*) grep -F 'Replace each received-ID placeholder with the corresponding ID from the handoff' "$prompt" >/dev/null || fail "$role does not require received-ID substitution" ;; esac
  [ "$role" != pc2-reviewer ] || grep -F 'Include the bracketed unit flag only when the handoff includes a unit ID' "$prompt" >/dev/null || fail 'reviewer optional unit syntax is ambiguous'
  for forbidden in 'THE FOUR MAXIMS' 'Allowed workflow commands:' 'correction budget' 'release map' 'Shared orchestration contract'; do
    grep -F "$forbidden" "$prompt" >/dev/null && fail "$role embeds obsolete manual content: $forbidden" || :
  done
done

daimon=$BOOTSTRAP_HOME/agents/daimon.md
aion=$BOOTSTRAP_HOME/agents/aion.md
grep -Fx 'tools: bash, subagent' "$daimon" >/dev/null || fail 'Pi Daimon bootstrap/delegation tools drifted'
grep -Fx 'maxSubagentDepth: 3' "$daimon" >/dev/null || fail 'Pi Daimon nesting depth drifted'
grep -F 'Handoff boundary: delegate only to aion.' "$daimon" >/dev/null || fail 'Pi Daimon target boundary drifted'
grep -F 'accept progress_update only from aion' "$daimon" >/dev/null || fail 'Pi Daimon supervisor wiring drifted'
grep -Fx 'tools: read, grep, find, ls, bash, edit, write, subagent' "$aion" >/dev/null || fail 'Pi Aion delegation tool drifted'
grep -Fx 'maxSubagentDepth: 3' "$aion" >/dev/null || fail 'Pi Aion nesting depth drifted'
targets='pc2-explorer, pc2-specifier, pc2-designer, pc2-task-planner, pc2-implementer, pc2-reviewer, pc2-sdd-initializer'
grep -F "Handoff boundary: delegate only to $targets; return to daimon." "$aion" >/dev/null || fail 'Pi Aion target boundary drifted'
grep -F 'send progress_update to daimon only through contact_supervisor' "$aion" >/dev/null || fail 'Pi Aion supervisor wiring drifted'
for role in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
  prompt=$BOOTSTRAP_HOME/agents/$role.md
  grep '^tools: .*subagent' "$prompt" >/dev/null && fail "$role can unexpectedly delegate in Pi" || :
  grep -F 'Handoff boundary: do not delegate; return to aion.' "$prompt" >/dev/null || fail "$role target boundary drifted"
done
rm -rf "$BOOTSTRAP_HOME"

verify_runtime_evidence() {
  node - "$1" "$2" "$3" <<'NODE'
const fs = require('fs');
const path = require('path');
const [tracePath, artifactsDir, mode] = process.argv.slice(2);
const ACK = 'AION_ACKNOWLEDGED_FACT';
const DELIVERY_ACK = 'AION_ACKNOWLEDGED_DELIVERY_FACT';
const TERMINAL = 'REVIEWER_TERMINAL_REVISION_2';
const TERMINAL_KEY = 'workflow:wf-smoke:terminal:2';
const DELIVERY_KEY = 'delivery:wf-smoke:published:3';
const WORKFLOW_COMPLETION = 'workflow wf-smoke completed at revision 2';
const BROADER_DELIVERY = 'broader delivery continues';
const NEXT_ACTION = 'next action: publish pull request';
const RAW = 'SPECIALIST_RAW_COMPLETION_DO_NOT_NARRATE';
const MARKER = 'DAIMON_USER_UPDATE_MARKER';
const FINAL_MARKER = 'FINAL_DELIVERY_UPDATE_MARKER';
const HOST_NOT_LIVE = 'PITCREW_HOST_NOT_LIVE';

const fail = message => { throw new Error(message); };
const object = (value, message) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail(message);
  return value;
};
const text = message => {
  object(message, 'Pi message record is malformed');
  if (typeof message.content === 'string') return message.content;
  if (!Array.isArray(message.content)) fail('Pi message content is malformed');
  return message.content.map(part => {
    object(part, 'Pi message content part is malformed');
    if (part.type !== 'text' || typeof part.text !== 'string') fail('Pi message contains non-text content');
    return part.text;
  }).join('');
};
const parseJsonl = (label, source) => {
  const lines = source.split(/\r?\n/).filter(Boolean);
  if (lines.length === 0) fail(`${label} is empty`);
  return lines.map((line, index) => {
    try { return object(JSON.parse(line), `${label}:${index + 1} is not an object`); }
    catch (error) { fail(`${label}:${index + 1} is not valid JSON: ${error.message}`); }
  });
};
const readJsonl = file => parseJsonl(file, fs.readFileSync(file, 'utf8'));
const timestamp = (value, label) => {
  if (typeof value !== 'string' || !Number.isFinite(Date.parse(value))) fail(`${label} has no valid timestamp`);
  return Date.parse(value);
};
const recursiveFiles = directory => {
  let entries;
  try { entries = fs.readdirSync(directory, { withFileTypes: true }); }
  catch (error) { fail(`cannot read Pi session artifacts: ${error.message}`); }
  return entries.flatMap(entry => {
    const file = path.join(directory, entry.name);
    return entry.isDirectory() ? recursiveFiles(file) : [file];
  });
};
const assistantEvents = events => events.map((event, index) => ({ event, index })).filter(({ event }) =>
  event.type === 'message_end' && event.message && event.message.role === 'assistant');

function verify(root, transcriptSources) {
  const headers = root.filter(event => event.type === 'session');
  if (headers.length !== 1 || typeof headers[0].id !== 'string' || !headers[0].id) fail('Pi root session identity is missing or ambiguous');
  for (const event of root) {
    if (typeof event.type !== 'string') fail('Pi event type is missing');
    if (event.type === 'message_end') object(event.message, 'Pi message_end record is malformed');
  }
  for (const { event } of assistantEvents(root)) {
    if (text(event.message).includes(RAW)) fail('Daimon user-facing assistant message exposed raw specialist prose');
  }
  const hostNotLive = assistantEvents(root).filter(({ event }) => text(event.message).trim() === HOST_NOT_LIVE);

  const transcripts = transcriptSources.map(({ file, events }) => {
    if (events.length === 0) fail(`${file} is empty`);
    const first = events[0];
    const agent = first.agent;
    const runId = first.runId;
    const childIndex = first.childIndex;
    if (typeof agent !== 'string' || !agent || typeof runId !== 'string' || !runId || !Number.isInteger(childIndex)) {
      fail(`${file} has no attributable agent/session identity`);
    }
    for (const event of events) {
      if (event.agent !== agent || event.runId !== runId || event.childIndex !== childIndex) {
        fail(`${file} changes agent/session identity`);
      }
      if (typeof event.recordType !== 'string' || typeof event.sourceEventType !== 'string') {
        fail(`${file} has an unattributable Pi event record`);
      }
    }
    return { file, events, agent, runId, childIndex };
  });
  const byAgent = agent => transcripts.filter(transcript => transcript.agent === agent);
  const [aion] = byAgent('aion');
  const [reviewer] = byAgent('pc2-reviewer');
  const [specialist] = byAgent('pc2-explorer');
  if (byAgent('aion').length !== 1 || byAgent('pc2-reviewer').length !== 1 ||
      byAgent('pc2-explorer').length !== 1 || transcripts.length !== 3) {
    fail('Pi child transcripts are missing, duplicated, or unattributable');
  }

  const aionAcknowledgements = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(ACK) && event.text.includes(TERMINAL_KEY));
  if (aionAcknowledgements.length !== 1) fail('expected one Aion acknowledgement event');
  const terminalResults = reviewer.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(TERMINAL) && event.text.includes(TERMINAL_KEY));
  if (terminalResults.length !== 1) fail('expected one attributable Reviewer terminal result');
  const calls = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'tool_start' && event.sourceEventType === 'tool_execution_start' && event.toolName === 'contact_supervisor');
  const parsedCalls = calls.map(call => {
    const callEnds = aion.events.filter(event =>
      event.recordType === 'tool_end' && event.sourceEventType === 'tool_execution_end' &&
      event.toolName === 'contact_supervisor' && event.toolCallId === call.event.toolCallId);
    if (callEnds.length !== 1 || callEnds[0].isError !== false) fail('injected Aion contact_supervisor did not complete successfully');
    try { return { ...call, args: object(JSON.parse(call.event.argsPayload), 'Aion contact_supervisor arguments are malformed') }; }
    catch (error) { fail(`Aion contact_supervisor arguments are malformed: ${error.message}`); }
  });
  const terminalCalls = parsedCalls.filter(call => typeof call.args.message === 'string' && call.args.message.includes(TERMINAL_KEY));
  if (terminalCalls.length !== 1) fail('expected one terminal Aion contact_supervisor call');
  const terminalCall = terminalCalls[0];
  const args = terminalCall.args;
  if (args.reason !== 'progress_update' || !args.message.includes(WORKFLOW_COMPLETION) ||
      !args.message.includes(BROADER_DELIVERY) || !args.message.includes(NEXT_ACTION) ||
      args.message.includes('delivery completed') || !args.message.includes(ACK)) {
    fail('Aion contact_supervisor arguments do not match the smoke contract');
  }
  if (aionAcknowledgements[0].index >= terminalCall.index ||
      timestamp(terminalResults[0].event.timestamp, 'Reviewer terminal result') >= timestamp(aionAcknowledgements[0].event.timestamp, 'Aion acknowledgement') ||
      timestamp(aionAcknowledgements[0].event.timestamp, 'Aion acknowledgement') >= timestamp(terminalCall.event.timestamp, 'Aion contact_supervisor call')) {
    fail('Reviewer terminal result, Aion acknowledgement, and relay call are out of order');
  }

  const specialistCalls = specialist.events.filter(event =>
    event.recordType === 'tool_start' && event.sourceEventType === 'tool_execution_start' && event.toolName === 'contact_supervisor');
  if (specialistCalls.length !== 0) fail('specialist contacted the supervisor directly');
  const rawOutputs = specialist.events.filter(event =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(RAW));
  if (rawOutputs.length !== 1) fail('expected one raw specialist output event');

  const relays = root.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.type === 'message_end' && event.message && event.message.role === 'custom' &&
    event.message.customType === 'subagent_supervisor_request');
  const publications = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'tool_start' && event.sourceEventType === 'tool_execution_start' && event.toolName === 'bash' &&
    typeof event.argsPayload === 'string' && event.argsPayload.includes('FIRST_PUBLICATION_ACTION'));
  if (hostNotLive.length > 0) {
    if (hostNotLive.length !== 1 || parsedCalls.length !== 1 || relays.length !== 0 ||
        assistantEvents(root).some(({ event }) => text(event.message).includes(MARKER) || text(event.message).includes(FINAL_MARKER)) ||
        publications.length !== 0 ||
        timestamp(terminalCall.event.timestamp, 'Aion contact_supervisor call') >= timestamp(hostNotLive[0].event.message.timestamp, 'Daimon host-liveness result')) {
      fail('host-liveness skip is not verified by an attributable Aion call and Daimon result');
    }
    return 'host-not-live';
  }
  if (parsedCalls.length !== 2 || relays.length !== 2) fail('expected one terminal relay and one independent delivery relay');
  const terminalRelays = relays.filter(({ event }) => text(event.message).includes(TERMINAL_KEY));
  const deliveryRelays = relays.filter(({ event }) => text(event.message).includes(DELIVERY_KEY));
  if (terminalRelays.length !== 1 || deliveryRelays.length !== 1) fail('native relays do not have distinct terminal and delivery keys');
  const relay = terminalRelays[0];
  const details = object(relay.event.message.details, 'native Aion progress relay details are malformed');
  if (details.reason !== 'progress_update' || details.agent !== 'aion' || details.runId !== aion.runId ||
      details.childIndex !== aion.childIndex || !text(relay.event.message).includes(ACK) ||
      !text(relay.event.message).includes(TERMINAL_KEY)) {
    fail('native progress relay does not identify the Aion session and acknowledgement');
  }
  if (timestamp(terminalCall.event.timestamp, 'Aion contact_supervisor call') >= timestamp(relay.event.message.timestamp, 'native Aion progress relay')) {
    fail('native Aion progress relay preceded its Aion call');
  }

  const markers = assistantEvents(root).filter(({ event }) => text(event.message).includes(MARKER));
  if (markers.length !== 1) fail(`expected one Daimon user marker, found ${markers.length}`);
  const marker = markers[0];
  if (text(marker.event.message).includes(RAW) || !text(marker.event.message).includes(WORKFLOW_COMPLETION) ||
      !text(marker.event.message).includes(BROADER_DELIVERY) || !text(marker.event.message).includes(NEXT_ACTION)) {
    fail('Daimon marker is raw, mislabels workflow completion, or omits the actual broader-delivery next action');
  }
  if (timestamp(relay.event.message.timestamp, 'native Aion progress relay') >= timestamp(marker.event.message.timestamp, 'Daimon user marker')) {
    fail('Daimon user marker preceded the native Aion progress relay');
  }
  if (publications.length !== 1 ||
      timestamp(marker.event.message.timestamp, 'Daimon user marker') >= timestamp(publications[0].event.timestamp, 'first publication action')) {
    fail('first publication action did not follow the Daimon workflow-complete message');
  }
  const deliveryAcknowledgements = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(DELIVERY_ACK) && event.text.includes(DELIVERY_KEY));
  const deliveryCalls = parsedCalls.filter(call => typeof call.args.message === 'string' && call.args.message.includes(DELIVERY_KEY));
  if (deliveryAcknowledgements.length !== 1 || deliveryCalls.length !== 1 ||
      deliveryCalls[0].args.reason !== 'progress_update' || !deliveryCalls[0].args.message.includes('delivery published') ||
      deliveryCalls[0].args.message.includes(TERMINAL_KEY) || deliveryCalls[0].args.message.includes(WORKFLOW_COMPLETION) ||
      timestamp(publications[0].event.timestamp, 'first publication action') >= timestamp(deliveryAcknowledgements[0].event.timestamp, 'Aion delivery acknowledgement') ||
      timestamp(deliveryAcknowledgements[0].event.timestamp, 'Aion delivery acknowledgement') >= timestamp(deliveryCalls[0].event.timestamp, 'Aion delivery relay call')) {
    fail('final delivery fact is not independently acknowledged and relayed after publication');
  }
  const deliveryRelay = deliveryRelays[0];
  const deliveryDetails = object(deliveryRelay.event.message.details, 'native Aion delivery relay details are malformed');
  if (deliveryDetails.reason !== 'progress_update' || deliveryDetails.agent !== 'aion' || deliveryDetails.runId !== aion.runId ||
      deliveryDetails.childIndex !== aion.childIndex || !text(deliveryRelay.event.message).includes(DELIVERY_ACK) ||
      timestamp(deliveryCalls[0].event.timestamp, 'Aion delivery relay call') >= timestamp(deliveryRelay.event.message.timestamp, 'native Aion delivery relay')) {
    fail('native delivery relay is unattributable or out of order');
  }
  const finalReports = assistantEvents(root).filter(({ event }) => text(event.message).includes(FINAL_MARKER));
  if (finalReports.length !== 1 || !text(finalReports[0].event.message).includes('delivery published') ||
      text(finalReports[0].event.message).includes(WORKFLOW_COMPLETION) || text(finalReports[0].event.message).includes(TERMINAL_KEY) ||
      timestamp(deliveryRelay.event.message.timestamp, 'native Aion delivery relay') >= timestamp(finalReports[0].event.message.timestamp, 'final delivery-only report')) {
    fail('final delivery-only report is missing, premature, or replays the workflow-terminal key');
  }
  return 'passed';
}

function verifyRetainedTurn(events) {
  let active = false;
  let activeAion = '';
  let terminal = '';
  let finalReports = 0;
  let quietNotice = false;
  let pendingSteer = '';
  let pendingGate = null;
  let gateAwaitingFact = false;
  let gateFactKey = '';
  const facts = new Set();
  const relayed = new Set();
  let prematureFinalAttempts = 0;
  let relaysAfterPrematureFinal = 0;
  for (const event of events) {
    object(event, 'retained-turn event is malformed');
    switch (event.type) {
    case 'start':
      if (active || terminal) fail('retained turn started more than once');
      active = true;
      if (event.aion !== undefined) {
        if (typeof event.aion !== 'string' || !event.aion) fail('active Aion identity is missing');
        activeAion = event.aion;
      }
      break;
    case 'quiet-notice':
      if (!active || quietNotice || !Number.isFinite(event.elapsedMinutes) || event.elapsedMinutes <= 0 || event.elapsedMinutes > 5) fail('quiet interval notice exceeded the finite five-minute bound');
      quietNotice = true;
      break;
    case 'fact':
      if (!active || typeof event.key !== 'string' || !event.key || facts.has(event.key)) fail('acknowledged fact is missing or duplicated');
      if (gateAwaitingFact) {
        if (!activeAion || event.aion !== activeAion) fail('post-gate fact did not come from the same active Aion');
        gateAwaitingFact = false;
        gateFactKey = event.key;
      }
      facts.add(event.key);
      quietNotice = false;
      break;
    case 'relay':
      if (!active || !facts.has(event.key) || relayed.has(event.key)) fail('fact relay is fabricated or replayed');
      relayed.add(event.key);
      if (prematureFinalAttempts > 0) relaysAfterPrematureFinal += 1;
      break;
    case 'steer':
      if (!active || pendingSteer || pendingGate || typeof event.request !== 'string' || !event.request) fail('steered input is not attributable');
      pendingSteer = event.request;
      break;
    case 'forward-requested':
      if (!active || event.request !== pendingSteer) fail('steered input was not forwarded as requested state');
      pendingSteer = '';
      quietNotice = false;
      break;
    case 'gate-presented':
      if (!active || pendingSteer || pendingGate || !activeAion || event.aion !== activeAion ||
          typeof event.gate !== 'string' || !event.gate) fail('clarification or approval gate was not presented by the active Aion');
      pendingGate = { gate: event.gate, answer: '', stage: 'presented' };
      break;
    case 'user-answer':
      if (!active || !pendingGate || pendingGate.stage !== 'presented' || event.gate !== pendingGate.gate ||
          typeof event.answer !== 'string' || !event.answer) fail('user gate answer is missing or unattributable');
      pendingGate.answer = event.answer;
      pendingGate.stage = 'answered';
      break;
    case 'answer-forwarded':
      if (!active || !pendingGate || pendingGate.stage !== 'answered' || event.aion !== activeAion ||
          event.gate !== pendingGate.gate || event.answer !== pendingGate.answer) fail('user answer was not forwarded to the same active Aion');
      pendingGate.stage = 'forwarded';
      break;
    case 'resume-wait':
      if (!active || !pendingGate || pendingGate.stage !== 'forwarded' || event.aion !== activeAion) fail('Daimon did not resume waiting on the same active Aion');
      pendingGate = null;
      gateAwaitingFact = true;
      quietNotice = false;
      break;
    case 'premature-final-attempt':
      if (!active || typeof event.outcome !== 'string' || !event.outcome) fail('premature final attempt is malformed');
      prematureFinalAttempts += 1;
      break;
    case 'terminal':
      if (!active || pendingSteer || pendingGate || gateAwaitingFact || !['completed', 'interrupted', 'cancelled', 'timed-out', 'failed', 'blocked', 'needs-user', 'user-owned-gate', 'abandoned'].includes(event.outcome)) fail('terminal outcome is invalid or premature');
      active = false;
      terminal = event.outcome;
      break;
    case 'final':
      if (active || !terminal || ++finalReports !== 1 || event.outcome !== terminal) fail('final response does not match one terminal outcome');
      break;
    default:
      fail(`unknown retained-turn event ${event.type}`);
    }
  }
  if (active || !terminal || finalReports !== 1) fail('retained turn ended without one terminal response');
  if (gateFactKey && !relayed.has(gateFactKey)) fail('post-gate Aion fact was not relayed exactly once');
  if (prematureFinalAttempts > 0 && relaysAfterPrematureFinal === 0) fail('premature final prevented a later queued milestone relay');
}

if (mode === 'regression' || mode === 'regression-red') {
  const injectedPrompt = `${ACK} ${RAW} ${MARKER} ${HOST_NOT_LIVE} contact_supervisor progress_update`;
  const promptOnly = parseJsonl('prompt-only fixture', [
    JSON.stringify({ type: 'session', version: 3, id: 'daimon-session', timestamp: '2026-01-01T00:00:00.000Z', cwd: '/fixture' }),
    JSON.stringify({ type: 'message_end', message: { role: 'user', content: [{ type: 'text', text: injectedPrompt }], timestamp: '2026-01-01T00:00:01.000Z' } }),
  ].join('\n'));
  const message = (role, value, timestamp) => ({ type: 'message_end', message: { role, content: [{ type: 'text', text: value }], timestamp } });
  const fixtureEvent = (agent, recordType, sourceEventType, timestamp, extra = {}) =>
    ({ agent, runId: `${agent}-run`, childIndex: agent === 'aion' ? 0 : 1, recordType, sourceEventType, timestamp, ...extra });
  const validRoot = [
    { type: 'session', version: 3, id: 'daimon-session', timestamp: '2026-01-01T00:00:00.000Z', cwd: '/fixture' },
    message('user', `submitted task input: ${RAW}`, '2026-01-01T00:00:01.000Z'),
    { type: 'message_end', message: { role: 'custom', customType: 'subagent_supervisor_request', content: [{ type: 'text', text: `${ACK}; ${TERMINAL_KEY}; ${WORKFLOW_COMPLETION}; ${BROADER_DELIVERY}; ${NEXT_ACTION}` }], details: { reason: 'progress_update', agent: 'aion', runId: 'aion-run', childIndex: 0 }, timestamp: '2026-01-01T00:00:04.000Z' } },
    message('assistant', `${MARKER}; ${WORKFLOW_COMPLETION}; ${BROADER_DELIVERY}; ${NEXT_ACTION}`, '2026-01-01T00:00:05.000Z'),
    { type: 'message_end', message: { role: 'custom', customType: 'subagent_supervisor_request', content: [{ type: 'text', text: `${DELIVERY_ACK}; ${DELIVERY_KEY}; delivery published; next action: none` }], details: { reason: 'progress_update', agent: 'aion', runId: 'aion-run', childIndex: 0 }, timestamp: '2026-01-01T00:00:08.000Z' } },
    message('assistant', `${FINAL_MARKER}; delivery published; next action: none`, '2026-01-01T00:00:09.000Z'),
  ];
  const transcripts = [
    { file: 'aion_transcript.jsonl', events: [
      fixtureEvent('aion', 'message', 'message_end', '2026-01-01T00:00:02.000Z', { role: 'assistant', text: `${ACK}; ${TERMINAL_KEY}` }),
      fixtureEvent('aion', 'tool_start', 'tool_execution_start', '2026-01-01T00:00:03.000Z', { toolName: 'contact_supervisor', toolCallId: 'call-1', argsPayload: JSON.stringify({ reason: 'progress_update', message: `${WORKFLOW_COMPLETION}; ${BROADER_DELIVERY}; ${NEXT_ACTION}; ${TERMINAL_KEY}; ${ACK}` }) }),
      fixtureEvent('aion', 'tool_end', 'tool_execution_end', '2026-01-01T00:00:03.500Z', { toolName: 'contact_supervisor', toolCallId: 'call-1', isError: false }),
      fixtureEvent('aion', 'tool_start', 'tool_execution_start', '2026-01-01T00:00:06.000Z', { toolName: 'bash', toolCallId: 'publish-1', argsPayload: '{"cmd":"printf FIRST_PUBLICATION_ACTION"}' }),
      fixtureEvent('aion', 'tool_end', 'tool_execution_end', '2026-01-01T00:00:06.500Z', { toolName: 'bash', toolCallId: 'publish-1', isError: false }),
      fixtureEvent('aion', 'message', 'message_end', '2026-01-01T00:00:07.000Z', { role: 'assistant', text: `${DELIVERY_ACK}; ${DELIVERY_KEY}` }),
      fixtureEvent('aion', 'tool_start', 'tool_execution_start', '2026-01-01T00:00:07.500Z', { toolName: 'contact_supervisor', toolCallId: 'call-2', argsPayload: JSON.stringify({ reason: 'progress_update', message: `${DELIVERY_ACK}; ${DELIVERY_KEY}; delivery published; next action: none` }) }),
      fixtureEvent('aion', 'tool_end', 'tool_execution_end', '2026-01-01T00:00:07.750Z', { toolName: 'contact_supervisor', toolCallId: 'call-2', isError: false }),
    ] },
    { file: 'pc2-reviewer_transcript.jsonl', events: [
      fixtureEvent('pc2-reviewer', 'message', 'message_end', '2026-01-01T00:00:01.500Z', { role: 'assistant', text: `${TERMINAL}; ${TERMINAL_KEY}` }),
    ] },
    { file: 'pc2-explorer_transcript.jsonl', events: [
      fixtureEvent('pc2-explorer', 'message', 'message_end', '2026-01-01T00:00:02.500Z', { role: 'assistant', text: RAW }),
    ] },
  ];
  if (mode === 'regression-red') verify(promptOnly, []);
  const assertRejected = (label, verifyFixture) => {
    try { verifyFixture(); }
    catch (_) { process.stdout.write(`${label}=rejected\n`); return; }
    fail(`${label} satisfied runtime evidence checks`);
  };

  const retainedTurn = [
    { type: 'start' },
    { type: 'quiet-notice', elapsedMinutes: 5 },
    { type: 'fact', key: 'workflow:wf-smoke:progress:1' },
    { type: 'relay', key: 'workflow:wf-smoke:progress:1' },
    { type: 'steer', request: 'publish after review' },
    { type: 'forward-requested', request: 'publish after review' },
    { type: 'quiet-notice', elapsedMinutes: 5 },
    { type: 'terminal', outcome: 'completed' },
    { type: 'final', outcome: 'completed' },
  ];
  verifyRetainedTurn(retainedTurn);
  process.stdout.write('retained-turn-with-steering=passed\n');
  const answerableGate = [
    { type: 'start', aion: 'aion-run' },
    { type: 'gate-presented', aion: 'aion-run', gate: 'approve publication' },
    { type: 'user-answer', gate: 'approve publication', answer: 'approved' },
    { type: 'answer-forwarded', aion: 'aion-run', gate: 'approve publication', answer: 'approved' },
    { type: 'resume-wait', aion: 'aion-run' },
    { type: 'fact', aion: 'aion-run', key: 'workflow:wf-smoke:terminal:gate-approved' },
    { type: 'relay', key: 'workflow:wf-smoke:terminal:gate-approved' },
    { type: 'terminal', outcome: 'completed' },
    { type: 'final', outcome: 'completed' },
  ];
  verifyRetainedTurn(answerableGate);
  process.stdout.write('answerable-gate-round-trip=passed\n');
  assertRejected('gate-answer-forwarded-to-different-aion', () => verifyRetainedTurn(answerableGate.map(event =>
    event.type === 'answer-forwarded' ? { ...event, aion: 'replacement-aion-run' } : event)));
  assertRejected('gate-answer-without-resumed-wait', () => verifyRetainedTurn(answerableGate.filter(event => event.type !== 'resume-wait')));
  assertRejected('replayed-retained-turn-fact', () => verifyRetainedTurn([
    ...retainedTurn.slice(0, 4),
    { type: 'relay', key: 'workflow:wf-smoke:progress:1' },
    ...retainedTurn.slice(4),
  ]));
  assertRejected('duplicate-quiet-notice', () => verifyRetainedTurn([
    retainedTurn[0], retainedTurn[1], retainedTurn[1], ...retainedTurn.slice(2),
  ]));
  assertRejected('late-quiet-notice', () => verifyRetainedTurn([
    retainedTurn[0], { type: 'quiet-notice', elapsedMinutes: 6 }, ...retainedTurn.slice(2),
  ]));
  const afterPrematureFinal = [
    { type: 'start' },
    { type: 'fact', key: 'workflow:wf-smoke:progress:1' },
    { type: 'relay', key: 'workflow:wf-smoke:progress:1' },
    { type: 'premature-final-attempt', outcome: 'completed' },
    { type: 'fact', key: 'workflow:wf-smoke:progress:2' },
    { type: 'relay', key: 'workflow:wf-smoke:progress:2' },
    { type: 'terminal', outcome: 'completed' },
    { type: 'final', outcome: 'completed' },
  ];
  verifyRetainedTurn(afterPrematureFinal);
  process.stdout.write('premature-final-later-milestone=passed\n');
  for (const outcome of ['interrupted', 'cancelled', 'timed-out', 'failed', 'blocked', 'needs-user', 'user-owned-gate', 'abandoned']) {
    verifyRetainedTurn([{ type: 'start' }, { type: 'terminal', outcome }, { type: 'final', outcome }]);
    process.stdout.write(`terminal-${outcome}=passed\n`);
  }
  if (verify(validRoot, transcripts) !== 'passed') fail('valid fixture did not satisfy runtime evidence checks');
  process.stdout.write('valid-runtime-evidence=passed\n');
  const silent = (label, rootEvents, transcriptEvents) => {
    const calls = transcriptEvents.flatMap(source => source.events).filter(event => event.recordType === 'tool_start' && event.toolName === 'contact_supervisor');
    const relays = rootEvents.filter(event => event.type === 'message_end' && event.message && event.message.customType === 'subagent_supervisor_request');
    const markers = assistantEvents(rootEvents).filter(({ event }) => text(event.message).includes(MARKER));
    if (calls.length !== 0 || relays.length !== 0 || markers.length !== 0) fail(`${label} emitted a report`);
    process.stdout.write(`${label}=silent\n`);
  };
  silent('fact-free-observation', promptOnly, []);
  silent('replayed-observation', [validRoot[0], validRoot[1]], [{ file: 'aion-replay', events: [transcripts[0].events[0]] }]);
  const absentRelayRoot = [validRoot[0], validRoot[1], message('assistant', HOST_NOT_LIVE, '2026-01-01T00:00:05.000Z')];
  const noPublicationTranscripts = [{ ...transcripts[0], events: transcripts[0].events.slice(0, 3) }, transcripts[1], transcripts[2]];
  if (verify(absentRelayRoot, noPublicationTranscripts) !== 'host-not-live') fail('absent live relay did not stay truthful');
  process.stdout.write('absent-live-relay=truthful\n');
  assertRejected('prompt-only-runtime-evidence', () => verify(promptOnly, []));
  assertRejected('separate-raw-leak-runtime-evidence', () =>
    verify([...validRoot, message('assistant', RAW, '2026-01-01T00:00:06.000Z')], transcripts));
  assertRejected('reviewer-terminal-after-relay', () => verify(validRoot, [
    transcripts[0],
    { ...transcripts[1], events: [{ ...transcripts[1].events[0], timestamp: '2026-01-01T00:00:04.500Z' }] },
    transcripts[2],
  ]));
  assertRejected('delivery-workflow-mislabel', () => verify(validRoot, [
    { ...transcripts[0], events: transcripts[0].events.map(event => event.toolCallId === 'call-1' && event.recordType === 'tool_start' ? { ...event, argsPayload: JSON.stringify({ reason: 'progress_update', message: `delivery completed; ${TERMINAL_KEY}; ${ACK}; next action: none` }) } : event) },
    transcripts[1], transcripts[2],
  ]));
  assertRejected('publication-before-daimon-message', () => verify(validRoot, [
    { ...transcripts[0], events: transcripts[0].events.map(event => event.toolCallId === 'publish-1' ? { ...event, timestamp: '2026-01-01T00:00:04.500Z' } : event) },
    transcripts[1], transcripts[2],
  ]));
  assertRejected('final-replays-terminal-key', () => verify(validRoot.map(event =>
    event.type === 'message_end' && event.message && text(event.message).includes(FINAL_MARKER)
      ? message('assistant', `${FINAL_MARKER}; delivery published; ${TERMINAL_KEY}`, '2026-01-01T00:00:09.000Z') : event), transcripts));
  assertRejected('fabricated-marker-without-relay', () => verify([...absentRelayRoot, message('assistant', `${MARKER}; ${WORKFLOW_COMPLETION}`, '2026-01-01T00:00:06.000Z')], noPublicationTranscripts));
  process.exit(0);
}

const root = readJsonl(tracePath);
const transcriptFiles = recursiveFiles(artifactsDir).filter(file => file.endsWith('_transcript.jsonl'));
if (transcriptFiles.length === 0) fail('Pi child transcripts are unavailable');
const result = verify(root, transcriptFiles.map(file => ({ file, events: readJsonl(file) })));
process.stdout.write(`${result}\n`);
NODE
}

case ${PITCREW_PI_SUPERVISOR_REGRESSION:-} in
  '') ;;
  red) verify_runtime_evidence '' '' regression-red ;;
  1) verify_runtime_evidence '' '' regression ;;
  *) fail 'PITCREW_PI_SUPERVISOR_REGRESSION must be 1 or red' ;;
esac
[ "${PITCREW_PI_SUPERVISOR_REGRESSION:-}" = '' ] || exit 0

[ "${PITCREW_PI_SUPERVISOR_SMOKE:-}" = 1 ] || skip 'set PITCREW_PI_SUPERVISOR_SMOKE=1 to run the real Pi supervisor smoke; steered dual-wait has no stable trace contract'
command -v pi >/dev/null 2>&1 || skip 'Pi executable is unavailable'
command -v node >/dev/null 2>&1 || skip 'Node.js is unavailable'

SOURCE_AGENT_HOME=${PI_AGENT_HOME:-${HOME:?HOME is required}/.pi/agent}
SOURCE_PACKAGE=$SOURCE_AGENT_HOME/npm/node_modules/pi-subagents
SOURCE_SETTINGS=$SOURCE_AGENT_HOME/settings.json
SOURCE_DEPTH=$SOURCE_AGENT_HOME/extensions/subagent/config.json
[ -r "$SOURCE_PACKAGE/package.json" ] || skip 'active official pi-subagents package is unavailable'
[ -r "$SOURCE_SETTINGS" ] || skip 'active official pi-subagents settings are unavailable'
[ -r "$SOURCE_DEPTH" ] || skip 'Pi nested delegation configuration is unavailable'

node - "$SOURCE_PACKAGE/package.json" "$SOURCE_SETTINGS" "$SOURCE_DEPTH" <<'NODE' || skip 'active official pi-subagents or nested delegation is not configured'
const fs = require('fs');
const [packagePath, settingsPath, depthPath] = process.argv.slice(2);
const read = path => JSON.parse(fs.readFileSync(path, 'utf8'));
const atLeast = (version, minimum) => {
  if (typeof version !== 'string' || !/^\d+\.\d+\.\d+$/.test(version)) return false;
  const actual = version.split('.').map(Number);
  const required = minimum.split('.').map(Number);
  for (let index = 0; index < required.length; index += 1) {
    if (actual[index] !== required[index]) return actual[index] > required[index];
  }
  return true;
};
try {
  const pkg = read(packagePath);
  const settings = read(settingsPath);
  const depth = read(depthPath);
  const active = settings && Array.isArray(settings.packages) && settings.packages.some(value =>
    typeof value === 'string' && (value === 'npm:pi-subagents' || value.startsWith('npm:pi-subagents@')));
  if (!pkg || pkg.name !== 'pi-subagents' || !atLeast(pkg.version, '0.25.0') || !active ||
      !depth || !Number.isInteger(depth.maxSubagentDepth) || depth.maxSubagentDepth < 3) process.exit(1);
} catch (_) { process.exit(1); }
NODE

case ${PITCREW_PI_SUPERVISOR_MODEL:-} in
  '') skip 'selected Pi model credentials are not configured; set PITCREW_PI_SUPERVISOR_MODEL' ;;
esac
if ! pi auth check --model "$PITCREW_PI_SUPERVISOR_MODEL" --json --no-refresh >/dev/null 2>&1; then
  skip 'selected Pi model credentials are unavailable'
fi
pi --help 2>&1 | grep -F -- '--mode <mode>' >/dev/null || skip 'Pi JSON event mode is unavailable'

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/pitcrew-pi-supervisor-smoke.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM
TEST_HOME=$TMP_ROOT/agent
TEST_SESSION=$TMP_ROOT/sessions
TEST_PROJECT=$TMP_ROOT/project
mkdir -p "$TEST_HOME/npm/node_modules" "$TEST_HOME/extensions/subagent" "$TEST_SESSION" "$TEST_PROJECT"
cp -R "$SOURCE_PACKAGE" "$TEST_HOME/npm/node_modules/pi-subagents"
chmod -R a-w "$TEST_HOME/npm/node_modules/pi-subagents"
printf '%s\n' '{"packages":["npm:pi-subagents"]}' > "$TEST_HOME/settings.json"
printf '%s\n' '{"maxSubagentDepth":3,"intercomBridge":{"mode":"always","resultDelivery":false}}' > "$TEST_HOME/extensions/subagent/config.json"

PI_AGENT_HOME=$TEST_HOME sh "$ROOT/scripts/install-templates.sh" pi >/dev/null || fail 'could not render isolated Pi definitions'
DAIMON_PROMPT=$(cat "$TEST_HOME/agents/daimon.md")
TRACE=$TMP_ROOT/events.jsonl
TASK='Use only the official subagent tool. Launch generated aion. Aion launches generated pc2-reviewer, which returns REVIEWER_TERMINAL_REVISION_2 and workflow:wf-smoke:terminal:2, and pc2-explorer, which returns SPECIALIST_RAW_COMPLETION_DO_NOT_NARRATE. Aion emits AION_ACKNOWLEDGED_FACT with that terminal key, then calls contact_supervisor once for that fact with workflow completion, broader delivery continues, and next action: publish pull request. Daimon emits one DAIMON_USER_UPDATE_MARKER from that relay. Only afterward Aion calls bash to print FIRST_PUBLICATION_ACTION, emits AION_ACKNOWLEDGED_DELIVERY_FACT with delivery:wf-smoke:published:3, and calls contact_supervisor once for that separate delivery published fact without the terminal key. Daimon emits one FINAL_DELIVERY_UPDATE_MARKER without replaying workflow completion or its terminal key. Replayed and fact-free observations stay silent. If the first live native relay is absent, output only PITCREW_HOST_NOT_LIVE; do not publish or emit either user marker.'

if ! (
  cd "$TEST_PROJECT"
  PI_AGENT_HOME=$TEST_HOME PI_CODING_AGENT_DIR=$TEST_HOME PI_SUBAGENT_TASK_DELIVERY=file \
    pi --mode json --print --model "$PITCREW_PI_SUPERVISOR_MODEL" --session-dir "$TEST_SESSION" \
      --no-context-files --tools subagent --system-prompt "$DAIMON_PROMPT" "$TASK"
) >"$TRACE" 2>&1; then
  fail 'Pi host smoke command failed'
fi
case $(verify_runtime_evidence "$TRACE" "$TEST_SESSION" smoke) in
  passed) ;;
  host-not-live) skip "Pi host cannot keep Daimon live as Aion's native parent; mid-flight native progress is unavailable" ;;
  *) fail 'Pi runtime evidence verifier returned an invalid result' ;;
esac

skip 'Pi supervisor relay passed, but steered user-input dual-wait has no stable native trace contract'
