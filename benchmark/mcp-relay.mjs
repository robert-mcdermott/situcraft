// The agent's MCP server during a benchmark run: relays its stdin and stdout to the socket the
// harness serves, where the harness's own "situcraft mcp" answers. The harness starts SituCraft,
// opens and prepares the task's project in its own browser, and only then starts the agent, which
// reaches the same SituCraft through this relay and sees nothing of how it was prepared. Adapted
// from CytoWeave 0.8.0's benchmark/mcp-relay.mjs (the same author's).
//
//   node benchmark/mcp-relay.mjs <socket path>

import { connect } from 'node:net';

const socket = connect(process.argv[2]);
socket.on('error', (error) => {
  process.stderr.write(`benchmark relay: ${error.message}\n`);
  process.exit(1);
});
process.stdin.pipe(socket);
socket.pipe(process.stdout);
socket.on('close', () => process.exit(0));
process.stdin.on('end', () => socket.end());
