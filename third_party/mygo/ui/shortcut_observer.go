package ui

// ShortcutEvent is an accepted native keyboard or submit action. All fields
// are values or copied strings; no Element, node, state or Context escapes
// into an audit listener.
type ShortcutEvent struct {
	Modifiers Modifiers
	Key       Key
	Repeat    bool
	Receiver  string
	ActionID  string
	Label     string
	// AlreadyRecorded is true only when the registering application also
	// explicitly reports this invocation to its own log. An observer may use it
	// to avoid writing the same command twice without hiding native events.
	AlreadyRecorded bool
}

// ShortcutDescriptor attaches semantic provenance to a subsequent binding.
// An empty ID still permits useful receiver information from native widgets.
type ShortcutDescriptor struct {
	ID              string
	Label           string
	AlreadyRecorded bool
}

func (rt *engine) describeShortcut(id uint64, mods Modifiers, key Key, descriptor ShortcutDescriptor) {
	if rt.shortcutObserver == nil {
		return
	}
	if rt.shortcutAnnotations == nil {
		rt.shortcutAnnotations = make(map[shortcutAnnotationKey]ShortcutDescriptor)
	}
	rt.shortcutAnnotations[shortcutAnnotationKey{id: id, mod: mods, key: key}] = descriptor
}

func (rt *engine) emitShortcut(id uint64, mods Modifiers, key Key, receiver string) {
	if rt.shortcutObserver == nil {
		return
	}
	event := ShortcutEvent{Modifiers: mods, Key: key, Repeat: rt.shortcutRepeat, Receiver: receiver}
	if description, ok := rt.shortcutAnnotations[shortcutAnnotationKey{id: id, mod: mods, key: key}]; ok {
		event.ActionID = description.ID
		event.Label = description.Label
		event.AlreadyRecorded = description.AlreadyRecorded
	}
	if event.Label == "" {
		event.Label = receiver
	}
	rt.shortcutObserver(event)
}

type shortcutAnnotationKey struct {
	id  uint64
	mod Modifiers
	key Key
}

// ObserveShortcuts observes matched native shortcut dispatches. observer is
// per runtime/window, and nil is the zero-overhead default that preserves
// ordinary MyGo behavior. It is safe to install the same callback every frame.
func (c *Context) ObserveShortcuts(observer func(ShortcutEvent)) {
	if c == nil || c.rt == nil {
		return
	}
	c.rt.shortcutObserver = observer
}

// DescribeShortcut annotates one binding on this element. Invoke it before
// executing the binding, as part of building the current frame. An observer
// can thereby distinguish application-specific actions from native widget
// navigation without retaining the Element beyond this call.
func (e Element) DescribeShortcut(descriptor ShortcutDescriptor, mods Modifiers, key Key) {
	n := e.node()
	if n == nil {
		return
	}
	n.c.rt.describeShortcut(n.id, mods, key, descriptor)
}

// DescribeShortcut is the Context equivalent of Element.DescribeShortcut.
// It uses the current parent, or the window for a root/window registration.
func (c *Context) DescribeShortcut(descriptor ShortcutDescriptor, mods Modifiers, key Key) {
	if c == nil || c.rt == nil {
		return
	}
	raw := c.build()
	if raw == nil || raw.parent == nil || raw.parent == raw.root {
		c.rt.describeShortcut(0, mods, key, descriptor)
		return
	}
	wrapElement(raw.parent).DescribeShortcut(descriptor, mods, key)
}
