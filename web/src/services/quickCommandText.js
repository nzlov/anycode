export function appendQuickCommand(prompt, command) {
  const currentPrompt = prompt.trim();
  const nextCommand = command.trim();
  if (!nextCommand) return currentPrompt;
  return currentPrompt ? `${currentPrompt}\n\n${nextCommand}` : nextCommand;
}
