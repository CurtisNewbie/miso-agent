```mermaid
graph TD
  compact_memory["compact_memory<br/>(ChatModel)"]
  end_node([end])
  prepare_messages("prepare_messages<br/>(Lambda)")
  remove_think("remove_think<br/>(Lambda)")
  start_node([start])
  compact_memory --> remove_think
  prepare_messages --> compact_memory
  remove_think --> end_node
  start_node --> prepare_messages

```