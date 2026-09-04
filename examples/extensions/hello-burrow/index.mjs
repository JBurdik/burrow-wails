const cwd = process.env.BURROW_EXTENSION_CWD || "no workspace";

console.log("Hello from a Burrow extension!");
console.log("Workspace: " + cwd);
console.log("Edit index.mjs, then run this command again.");
