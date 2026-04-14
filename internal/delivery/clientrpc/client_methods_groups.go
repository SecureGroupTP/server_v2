package clientrpc

import "context"

func (h *Handler) handleProfileMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "getProfile":
		return h.clientService.GetProfile(ctx, mustBytes(params, "userPublicKey"))
	case "updateProfile":
		return h.clientService.UpdateProfile(ctx, state.UserPublicKey, optionalString(params, "username"), optionalString(params, "displayName"), optionalString(params, "avatarHash"), optionalString(params, "bio"))
	case "searchProfiles":
		return h.clientService.SearchProfiles(ctx, optionalString(params, "query"), optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "deleteAccount":
		return h.clientService.DeleteAccount(ctx, state.UserPublicKey)
	case "getProfileAvatar":
		return h.clientService.GetProfileAvatar(ctx, mustBytes(params, "userPublicKey"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleDeviceAndMLSMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "listDevices":
		return h.clientService.ListDevices(ctx, state.UserPublicKey)
	case "registerDevicePushToken":
		return h.clientService.RegisterDevicePushToken(ctx, state.SessionID, state.UserPublicKey, int16(optionalInt(params, "platform")), optionalString(params, "pushToken"), optionalBool(params, "isEnabled"))
	case "removeDevice":
		return h.clientService.RemoveDevice(ctx, state.UserPublicKey, mustUUID(params, "deviceId"))
	case "uploadKeyPackages":
		return h.clientService.UploadKeyPackages(ctx, state.SessionID, state.UserPublicKey, mustMapList(params, "packages"))
	case "fetchKeyPackages":
		return h.clientService.FetchKeyPackages(ctx, mustBytesList(params, "userPublicKeys"))
	case "sendCommit":
		return h.clientService.SendCommit(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "commitBytes"))
	case "sendWelcome":
		return h.clientService.SendWelcome(ctx, state.UserPublicKey, optionalUUIDPtr(params, "roomId"), mustBytes(params, "targetUserPublicKey"), mustBytes(params, "welcomeBytes"))
	case "uploadGroupInfo":
		return h.clientService.UploadGroupInfo(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "groupInfoBytes"))
	case "fetchGroupInfo":
		return h.clientService.FetchGroupInfo(ctx, mustUUID(params, "roomId"))
	case "sendExternalCommit":
		return h.clientService.SendExternalCommit(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "commitBytes"))
	case "fetchWelcome":
		return h.clientService.FetchWelcome(ctx, state.UserPublicKey, mustUUID(params, "roomId"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleFriendMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "listFriends":
		return h.clientService.ListFriends(ctx, state.UserPublicKey, optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "removeFriend":
		return h.clientService.RemoveFriend(ctx, state.UserPublicKey, mustBytes(params, "friendPublicKey"))
	case "sendFriendRequest":
		return h.clientService.SendFriendRequest(ctx, state.UserPublicKey, mustBytes(params, "receiverPublicKey"))
	case "acceptFriendRequest":
		return h.clientService.AcceptFriendRequest(ctx, state.UserPublicKey, mustUUID(params, "requestId"))
	case "declineFriendRequest":
		return h.clientService.DeclineFriendRequest(ctx, state.UserPublicKey, mustUUID(params, "requestId"))
	case "cancelFriendRequest":
		return h.clientService.CancelFriendRequest(ctx, state.UserPublicKey, mustUUID(params, "requestId"))
	case "listFriendRequests":
		return h.clientService.ListFriendRequests(ctx, state.UserPublicKey, optionalString(params, "direction"), optionalInt(params, "limit"), optionalString(params, "cursor"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleRoomMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "createChatRoom":
		return h.clientService.CreateChatRoom(ctx, state.UserPublicKey, optionalString(params, "title"), optionalString(params, "description"), int16(optionalInt(params, "visibility")))
	case "createDirectRoom":
		return h.clientService.CreateDirectRoom(ctx, state.UserPublicKey, mustBytes(params, "targetUserPublicKey"))
	case "listChatRooms":
		return h.clientService.ListChatRooms(ctx, state.UserPublicKey, optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "getChatRoom":
		return h.clientService.GetChatRoom(ctx, mustUUID(params, "roomId"))
	case "searchChatRooms":
		return h.clientService.SearchChatRooms(ctx, optionalString(params, "query"), optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "syncChatRoom":
		return h.clientService.SyncChatRoom(ctx, mustUUID(params, "roomId"))
	case "updateChatRoom":
		return h.clientService.UpdateChatRoom(ctx, state.UserPublicKey, mustUUID(params, "roomId"), optionalString(params, "title"), optionalString(params, "description"), optionalString(params, "avatarHash"))
	case "updateChatRoomState":
		return h.clientService.UpdateChatRoomState(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustUUID(params, "groupId"), int64(optionalInt(params, "epoch")), mustBytes(params, "treeBytes"), mustBytes(params, "treeHash"))
	case "fetchChatRoomState":
		return h.clientService.FetchChatRoomState(ctx, state.UserPublicKey, mustUUID(params, "roomId"), int64(optionalInt(params, "epoch")))
	case "deleteChatRoom":
		return h.clientService.DeleteChatRoom(ctx, state.UserPublicKey, mustUUID(params, "roomId"))
	case "getChatRoomAvatar":
		return h.clientService.GetChatRoomAvatar(ctx, mustUUID(params, "roomId"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleMemberMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "joinChatRoom":
		return h.clientService.JoinChatRoom(ctx, state.UserPublicKey, mustUUID(params, "roomId"))
	case "leaveChatRoom":
		return h.clientService.LeaveChatRoom(ctx, state.UserPublicKey, mustUUID(params, "roomId"))
	case "kickChatMember":
		return h.clientService.KickChatMember(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "userPublicKey"))
	case "listChatMembers":
		return h.clientService.ListChatMembers(ctx, mustUUID(params, "roomId"), optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "updateChatMemberRole":
		return h.clientService.UpdateChatMemberRole(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "userPublicKey"), int16(optionalInt(params, "role")))
	case "createChatMemberPermission":
		return h.clientService.CreateChatMemberPermission(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "userPublicKey"), optionalString(params, "permissionKey"), optionalBool(params, "isAllowed"))
	case "listChatMemberPermissions":
		return h.clientService.ListChatMemberPermissions(ctx, mustUUID(params, "roomId"), optionalBytes(params, "userPublicKey"), optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "updateChatMemberPermission":
		return h.clientService.UpdateChatMemberPermission(ctx, state.UserPublicKey, mustUUID(params, "permissionId"), optionalBool(params, "isAllowed"))
	case "deleteChatMemberPermission":
		return h.clientService.DeleteChatMemberPermission(ctx, state.UserPublicKey, mustUUID(params, "permissionId"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleInvitationMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "sendChatInvitation":
		return h.clientService.SendChatInvitation(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustBytes(params, "inviteePublicKey"), optionalTimePtr(params, "expiresAt"), optionalBytes(params, "inviteToken"), optionalBytes(params, "inviteTokenSignature"))
	case "revokeChatInvitation":
		return h.clientService.RevokeChatInvitation(ctx, state.UserPublicKey, mustUUID(params, "invitationId"))
	case "listSentChatInvitations":
		return h.clientService.ListSentChatInvitations(ctx, state.UserPublicKey, optionalUUIDPtr(params, "roomId"), optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "listIncomingChatInvitations":
		return h.clientService.ListIncomingChatInvitations(ctx, state.UserPublicKey, optionalInt(params, "limit"), optionalString(params, "cursor"))
	case "acceptChatInvitation":
		return h.clientService.AcceptChatInvitation(ctx, state.UserPublicKey, mustUUID(params, "invitationId"), optionalBytes(params, "commitBytes"))
	case "declineChatInvitation":
		return h.clientService.DeclineChatInvitation(ctx, state.UserPublicKey, mustUUID(params, "invitationId"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleMessageMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "sendMessage":
		return h.clientService.SendMessage(ctx, state.UserPublicKey, mustUUID(params, "roomId"), mustUUID(params, "clientMsgId"), mustBytesList(params, "body"))
	default:
		return nil, nil
	}
}

func (h *Handler) handleOverviewMethods(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "getServerLimits":
		return h.clientService.GetServerLimits(ctx)
	case "getUserLimits":
		return h.clientService.GetUserLimits(ctx, state.UserPublicKey)
	case "getGroupLimits":
		return h.clientService.GetGroupLimits(ctx, mustUUID(params, "roomId"))
	case "getServerConfig":
		return h.clientService.GetServerConfig(), nil
	default:
		return nil, nil
	}
}
