```mermaid
graph TD
  clarify_generate_research["clarify_generate_research<br/>(ChatModel)"]
  end_node([end])
  extract_tool_output("extract_tool_output<br/>(Lambda)")
  prepare_messages("prepare_messages<br/>(Lambda)")
  start_node([start])
  tools["tools<br/>(ToolsNode)"]
  clarify_generate_research --> tools
  extract_tool_output --> end_node
  prepare_messages --> clarify_generate_research
  start_node --> prepare_messages
  tools --> extract_tool_output

```