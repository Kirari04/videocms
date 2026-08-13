package storage

// MediaLayout centralizes the mapping between domain identifiers and object
// keys. The initial layout matches the existing on-disk tree so introducing
// the abstraction does not move any user data.
type MediaLayout interface {
	Source(fileUUID, fileName string) (Key, error)
	Video(fileUUID, quality, fileName string) (Key, error)
	VideoPrefix(fileUUID, quality string) (Key, error)
	Audio(fileUUID, audioUUID, fileName string) (Key, error)
	AudioPrefix(fileUUID, audioUUID string) (Key, error)
	Subtitle(fileUUID, subtitleUUID, fileName string) (Key, error)
	SubtitlePrefix(fileUUID, subtitleUUID string) (Key, error)
	Thumbnail(fileUUID, fileName string) (Key, error)
	FilePrefix(fileUUID string) (Key, error)
}

type LegacyMediaLayout struct{}

func (LegacyMediaLayout) Source(fileUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, "source", fileName)
}

func (LegacyMediaLayout) Video(fileUUID, quality, fileName string) (Key, error) {
	return JoinKey(fileUUID, quality, fileName)
}

func (LegacyMediaLayout) VideoPrefix(fileUUID, quality string) (Key, error) {
	return JoinKey(fileUUID, quality)
}

func (LegacyMediaLayout) Audio(fileUUID, audioUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, audioUUID, fileName)
}

func (LegacyMediaLayout) AudioPrefix(fileUUID, audioUUID string) (Key, error) {
	return JoinKey(fileUUID, audioUUID)
}

func (LegacyMediaLayout) Subtitle(fileUUID, subtitleUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, subtitleUUID, fileName)
}

func (LegacyMediaLayout) SubtitlePrefix(fileUUID, subtitleUUID string) (Key, error) {
	return JoinKey(fileUUID, subtitleUUID)
}

func (LegacyMediaLayout) Thumbnail(fileUUID, fileName string) (Key, error) {
	return JoinKey(fileUUID, fileName)
}

func (LegacyMediaLayout) FilePrefix(fileUUID string) (Key, error) {
	return JoinKey(fileUUID)
}
