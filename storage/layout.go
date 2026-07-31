package storage

// MediaLayout centralizes the mapping between domain identifiers and object
// keys. The initial layout matches the existing on-disk tree so introducing
// the abstraction does not move any user data.
type MediaLayout interface {
	Video(fileUUID, quality, fileName string) (Key, error)
	Audio(fileUUID, audioUUID, fileName string) (Key, error)
	Subtitle(fileUUID, subtitleUUID, fileName string) (Key, error)
	Thumbnail(fileUUID, fileName string) (Key, error)
	FilePrefix(fileUUID string) (Key, error)
}

type LegacyMediaLayout struct{}

func (LegacyMediaLayout) Video(fileUUID, quality, fileName string) (Key, error) {
	return JoinKey(fileUUID, quality, fileName)
}

func (LegacyMediaLayout) Audio(fileUUID, audioUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, audioUUID, fileName)
}

func (LegacyMediaLayout) Subtitle(fileUUID, subtitleUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, subtitleUUID, fileName)
}

func (LegacyMediaLayout) Thumbnail(fileUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, fileName)
}

func (LegacyMediaLayout) FilePrefix(fileUUID string) (Key, error) {
	return JoinKey(fileUUID)
}
