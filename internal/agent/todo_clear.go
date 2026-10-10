package agent

import "github.com/ziyan/teanode/internal/db"

// clearFinishedTodos takes the conversation's task list away when a turn
// ends with every step on it done: the work it tracked is over, and a
// list of ticks left above the box says nothing. A list with a step still
// open stays, for the turn that picks it up. Before the turn's done event,
// on which the drawer reads the list again.
func (self *AskRun) clearFinishedTodos() {
	conversationId := self.settings.Conversation.ID
	err := self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
		todos, err := tx.ListAgentTodos(conversationId)
		if err != nil || len(todos) == 0 {
			return err
		}
		for _, todo := range todos {
			if todo.DoneAt == nil {
				return nil
			}
		}
		for _, todo := range todos {
			if err := tx.DeleteAgentTodo(conversationId, todo.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Warningf("cannot clear the finished task list of conversation %s: %s", conversationId, err)
	}
}
