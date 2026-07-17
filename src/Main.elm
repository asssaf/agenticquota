port module Main exposing (main)

import Browser
import Dict exposing (Dict)
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Http
import Iso8601
import Json.Decode as Decode exposing (Decoder)
import Json.Encode as Encode
import Set exposing (Set)
import Svg exposing (Svg)
import Svg.Attributes as SvgAttr
import Svg.Events as SvgEvent
import Task
import Time

-- PORTS
port saveApiKey : { key : String } -> Cmd msg
port saveAutoRefresh : Int -> Cmd msg
port onChartMouseMove : (Float -> msg) -> Sub msg
port onChartMouseLeave : (() -> msg) -> Sub msg

-- TYPES
type Tab
    = Overview
    | History

type ConnectionStatus
    = Connected
    | Unauthorized
    | Offline
    | AuthRequired

type alias Quota =
    { remainingFraction : Float
    , resetTime : String
    , resetInSeconds : Int
    }

type alias HistoryPoint =
    { timestamp : String
    , value : Float
    , isReset : Bool
    }

type alias ResetPoint =
    { resetTime : String }

-- MODEL
type alias Model =
    { apiKey : String
    , quotas : List (String, Quota)
    , fetchTime : Time.Posix
    , history : List (String, List HistoryPoint)
    , activeTab : Tab
    , selectedDays : Int
    , toggledOffQuotas : Set String
    , autoRefreshInterval : Int
    , currentTime : Time.Posix
    , timeZone : Time.Zone
    , isFetching : Bool
    , alertMessage : Maybe (String, Bool) -- (Message, IsSuccess)
    , showSettingsModal : Bool
    , tempApiKey : String
    , tempSaveKey : Bool
    , connectionStatus : ConnectionStatus
    -- Chart hover tracking
    , hoverX : Maybe Float
    }

type alias Flags =
    { apiKey : String
    , autoRefresh : Int
    }

-- INIT
init : Flags -> ( Model, Cmd Msg )
init flags =
    let
        initialStatus =
            if String.isEmpty flags.apiKey then
                AuthRequired
            else
                Offline

        model =
            { apiKey = flags.apiKey
            , quotas = []
            , fetchTime = Time.millisToPosix 0
            , history = []
            , activeTab = Overview
            , selectedDays = 1
            , toggledOffQuotas = Set.empty
            , autoRefreshInterval = flags.autoRefresh
            , currentTime = Time.millisToPosix 0
            , timeZone = Time.utc
            , isFetching = False
            , alertMessage =
                if String.isEmpty flags.apiKey then
                    Just ( "API Key is required to view quota metrics.", False )
                else
                    Nothing
            , showSettingsModal = String.isEmpty flags.apiKey
            , tempApiKey = flags.apiKey
            , tempSaveKey = True
            , connectionStatus = initialStatus
            , hoverX = Nothing
            }

        initialCmds =
            [ Task.perform AdjustTimeZone Time.here
            , Task.perform Tick Time.now
            ]

        cmds =
            if String.isEmpty flags.apiKey then
                initialCmds
            else
                initialCmds ++ [ fetchQuotaData flags.apiKey ]
    in
    ( model, Cmd.batch cmds )

-- MSG
type Msg
    = AdjustTimeZone Time.Zone
    | Tick Time.Posix
    | TriggerRefresh
    | SetActiveTab Tab
    | SetSelectedDays Int
    | ToggleQuotaSeries String
    | ChangeAutoRefresh Int
    | ShowSettingsModal Bool
    | InputApiKey String
    | CheckboxSaveKey Bool
    | SubmitSettings
    | FetchQuotaResult (Result Http.Error (List (String, Quota)))
    | FetchHistoryResult (Result Http.Error (List (String, List HistoryPoint)))
    | FetchResetHistoryResult (List (String, List HistoryPoint)) (Result Http.Error (List (String, List ResetPoint)))
    | MouseMove Float
    | MouseLeave
    | DismissAlert

-- UPDATE
update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        AdjustTimeZone zone ->
            ( { model | timeZone = zone }, Cmd.none )

        Tick posix ->
            ( { model | currentTime = posix }, Cmd.none )

        TriggerRefresh ->
            if model.isFetching then
                ( model, Cmd.none )
            else
                let
                    cmd =
                        case model.activeTab of
                            Overview ->
                                fetchQuotaData model.apiKey

                            History ->
                                fetchHistoryData model.apiKey model.selectedDays
                in
                ( { model | isFetching = True }, cmd )

        SetActiveTab tab ->
            let
                newModel =
                    { model | activeTab = tab, hoverX = Nothing, isFetching = True }

                cmd =
                    case tab of
                        Overview ->
                            fetchQuotaData model.apiKey

                        History ->
                            fetchHistoryData model.apiKey model.selectedDays
            in
            ( newModel, cmd )

        SetSelectedDays days ->
            let
                newModel =
                    { model | selectedDays = days, hoverX = Nothing, isFetching = True }
            in
            ( newModel, fetchHistoryData model.apiKey days )

        ToggleQuotaSeries name ->
            let
                allKeys =
                    List.map Tuple.first model.history

                enabledKeys =
                    List.filter (\k -> not (Set.member k model.toggledOffQuotas)) allKeys

                newToggled =
                    if Set.member name model.toggledOffQuotas then
                        Set.remove name model.toggledOffQuotas
                    else if List.length enabledKeys > 1 then
                        Set.insert name model.toggledOffQuotas
                    else
                        model.toggledOffQuotas
            in
            ( { model | toggledOffQuotas = newToggled }, Cmd.none )

        ChangeAutoRefresh val ->
            ( { model | autoRefreshInterval = val }
            , saveAutoRefresh val
            )

        ShowSettingsModal show ->
            ( { model | showSettingsModal = show }, Cmd.none )

        InputApiKey key ->
            ( { model | tempApiKey = key }, Cmd.none )

        CheckboxSaveKey save ->
            ( { model | tempSaveKey = save }, Cmd.none )

        SubmitSettings ->
            let
                newKey =
                    String.trim model.tempApiKey

                saveCmd =
                    if model.tempSaveKey then
                        saveApiKey { key = newKey }
                    else
                        saveApiKey { key = "" }

                newModel =
                    { model | apiKey = newKey, showSettingsModal = False, isFetching = True, alertMessage = Nothing }
            in
            ( newModel
            , Cmd.batch [ saveCmd, fetchQuotaData newKey ]
            )

        FetchQuotaResult result ->
            let
                fetchedTime =
                    model.currentTime
            in
            case result of
                Ok quotas ->
                    ( { model | quotas = quotas, fetchTime = fetchedTime, connectionStatus = Connected, isFetching = False, alertMessage = Nothing }
                    , Cmd.none
                    )

                Err err ->
                    let
                        ( status, msgStr ) =
                            handleHttpError err
                    in
                    ( { model | connectionStatus = status, isFetching = False, alertMessage = Just ( msgStr, False ), showSettingsModal = if status == Unauthorized then True else model.showSettingsModal }
                    , Cmd.none
                    )

        FetchHistoryResult result ->
            case result of
                Ok historyList ->
                    ( model
                    , fetchResetHistoryData model.apiKey model.selectedDays historyList
                    )

                Err err ->
                    let
                        ( status, msgStr ) =
                            handleHttpError err
                    in
                    ( { model | connectionStatus = status, isFetching = False, alertMessage = Just ( msgStr, False ) }
                    , Cmd.none
                    )

        FetchResetHistoryResult historyList result ->
            case result of
                Ok resetList ->
                    let
                        mergedHistory =
                            mergeHistoryAndResets historyList resetList
                    in
                    ( { model | history = mergedHistory, connectionStatus = Connected, isFetching = False, alertMessage = Nothing }
                    , Cmd.none
                    )

                Err err ->
                    let
                        ( status, msgStr ) =
                            handleHttpError err
                    in
                    ( { model | connectionStatus = status, isFetching = False, alertMessage = Just ( msgStr, False ) }
                    , Cmd.none
                    )

        MouseMove x ->
            ( { model | hoverX = Just x }, Cmd.none )

        MouseLeave ->
            ( { model | hoverX = Nothing }, Cmd.none )

        DismissAlert ->
            ( { model | alertMessage = Nothing }, Cmd.none )

-- HELPERS
connectionStatusText : ConnectionStatus -> String
connectionStatusText status =
    case status of
        Connected -> "Connected"
        Unauthorized -> "Unauthorized"
        Offline -> "Offline"
        AuthRequired -> "Authentication Required"

connectionStatusClass : ConnectionStatus -> String
connectionStatusClass status =
    case status of
        Connected -> "status-connected"
        Unauthorized -> "status-unauthorized"
        Offline -> "status-offline"
        AuthRequired -> "status-offline"

handleHttpError : Http.Error -> ( ConnectionStatus, String )
handleHttpError err =
    case err of
        Http.BadStatus 401 ->
            ( Unauthorized, "Unauthorized API Key. Please verify your X-API-Key settings." )

        Http.BadStatus 404 ->
            -- If 404, we assume connected but empty
            ( Connected, "Quota data endpoint not found (404)." )

        Http.BadStatus code ->
            ( Offline, "Server error: status code " ++ String.fromInt code )

        Http.BadUrl _ ->
            ( Offline, "Internal App Error: invalid API route URL." )

        Http.Timeout ->
            ( Offline, "Connection timeout. Ensure the server is reachable." )

        Http.NetworkError ->
            ( Offline, "Cannot connect to quota service. Ensure the server is running." )

        Http.BadBody bodyErr ->
            ( Offline, "Failed to parse server response: " ++ bodyErr )

mergeHistoryAndResets : List ( String, List HistoryPoint ) -> List ( String, List ResetPoint ) -> List ( String, List HistoryPoint )
mergeHistoryAndResets historyList resetList =
    let
        resetDict =
            Dict.fromList resetList

        mergeOne ( name, pts ) =
            let
                resets =
                    Dict.get name resetDict
                        |> Maybe.withDefault []

                resetPts =
                    List.map (\r -> { timestamp = r.resetTime, value = 1.0, isReset = True }) resets

                allPts =
                    pts ++ resetPts
            in
            ( name, List.sortBy .timestamp allPts )
    in
    List.map mergeOne historyList

-- HTTP REQUESTS
quotaDecoder : Decoder Quota
quotaDecoder =
    Decode.map3 Quota
        (Decode.field "remaining_fraction" Decode.float)
        (Decode.field "reset_time" Decode.string)
        (Decode.field "reset_in_seconds" Decode.int)

quotaResponseDecoder : Decoder (List (String, Quota))
quotaResponseDecoder =
    Decode.oneOf
        [ Decode.field "quota" (Decode.keyValuePairs quotaDecoder)
        , Decode.succeed []
        ]

historyPointDecoder : Decoder HistoryPoint
historyPointDecoder =
    Decode.map3 HistoryPoint
        (Decode.field "timestamp" Decode.string)
        (Decode.field "value" Decode.float)
        (Decode.succeed False)

historyResponseDecoder : Decoder (List (String, List HistoryPoint))
historyResponseDecoder =
    Decode.oneOf
        [ Decode.field "history" (Decode.keyValuePairs (Decode.list historyPointDecoder))
        , Decode.succeed []
        ]

resetPointDecoder : Decoder ResetPoint
resetPointDecoder =
    Decode.map ResetPoint
        (Decode.field "reset_time" Decode.string)

resetHistoryResponseDecoder : Decoder (List (String, List ResetPoint))
resetHistoryResponseDecoder =
    Decode.oneOf
        [ Decode.field "history" (Decode.keyValuePairs (Decode.list resetPointDecoder))
        , Decode.succeed []
        ]

fetchQuotaData : String -> Cmd Msg
fetchQuotaData apiKey =
    Http.request
        { method = "GET"
        , headers = [ Http.header "X-API-Key" apiKey ]
        , url = "/api/v1/quota"
        , body = Http.emptyBody
        , expect = Http.expectJson FetchQuotaResult quotaResponseDecoder
        , timeout = Nothing
        , tracker = Nothing
        }

fetchHistoryData : String -> Int -> Cmd Msg
fetchHistoryData apiKey days =
    Http.request
        { method = "GET"
        , headers = [ Http.header "X-API-Key" apiKey ]
        , url = "/api/v1/quota/history?days=" ++ String.fromInt days
        , body = Http.emptyBody
        , expect = Http.expectJson FetchHistoryResult historyResponseDecoder
        , timeout = Nothing
        , tracker = Nothing
        }

fetchResetHistoryData : String -> Int -> List (String, List HistoryPoint) -> Cmd Msg
fetchResetHistoryData apiKey days historyList =
    Http.request
        { method = "GET"
        , headers = [ Http.header "X-API-Key" apiKey ]
        , url = "/api/v1/quota/history/reset?days=" ++ String.fromInt days
        , body = Http.emptyBody
        , expect = Http.expectJson (FetchResetHistoryResult historyList) resetHistoryResponseDecoder
        , timeout = Nothing
        , tracker = Nothing
        }

-- VIEW
view : Model -> Html Msg
view model =
    div []
        [ div [ class "bg-glow" ] []
        , renderHeader model
        , renderMain model
        , renderModal model
        , renderFooter model
        ]

renderHeader : Model -> Html Msg
renderHeader model =
    header [ class "dashboard-header" ]
        [ div [ class "header-container" ]
            [ div [ class "logo-group" ]
                [ span [ class "logo-icon" ] [ text "▲" ]
                , div [ class "title-meta" ]
                    [ h1 [] [ text "Antigravity Quota" ]
                    , p [ class "subtitle" ] [ text "Agent Resource Telemetry" ]
                    ]
                ]
            , nav [ class "dashboard-nav" ]
                [ button
                    [ id "tab-active"
                    , classList [ ( "nav-tab", True ), ( "active", model.activeTab == Overview ) ]
                    , onClick (SetActiveTab Overview)
                    ]
                    [ text "Overview" ]
                , button
                    [ id "tab-history"
                    , classList [ ( "nav-tab", True ), ( "active", model.activeTab == History ) ]
                    , onClick (SetActiveTab History)
                    ]
                    [ text "History Graphs" ]
                ]
            , div [ class "header-controls" ]
                [ div [ class "status-indicator-group" ]
                    [ span
                        [ id "conn-indicator"
                        , class ("status-dot " ++ connectionStatusClass model.connectionStatus)
                        ]
                        []
                    , span [ id "conn-text" , class "status-text" ]
                        [ text (connectionStatusText model.connectionStatus) ]
                    ]
                , button
                    [ id "settings-btn"
                    , class "icon-btn"
                    , title "API Configuration"
                    , attribute "aria-label" "Settings"
                    , onClick (ShowSettingsModal True)
                    ]
                    [ Svg.svg
                        [ SvgAttr.viewBox "0 0 24 24"
                        , SvgAttr.width "20"
                        , SvgAttr.height "20"
                        , SvgAttr.stroke "currentColor"
                        , SvgAttr.strokeWidth "2"
                        , SvgAttr.fill "none"
                        , SvgAttr.strokeLinecap "round"
                        , SvgAttr.strokeLinejoin "round"
                        ]
                        [ Svg.circle [ SvgAttr.cx "12", SvgAttr.cy "12", SvgAttr.r "3" ] []
                        , Svg.path [ SvgAttr.d "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" ] []
                        ]
                    ]
                , button
                    [ id "refresh-btn"
                    , class "primary-btn"
                    , title "Refresh metrics"
                    , attribute "aria-label" "Refresh"
                    , disabled model.isFetching
                    , onClick TriggerRefresh
                    ]
                    [ Svg.svg
                        [ id "refresh-icon"
                        , SvgAttr.class (if model.isFetching then "spinning" else "")
                        , SvgAttr.viewBox "0 0 24 24"
                        , SvgAttr.width "18"
                        , SvgAttr.height "18"
                        , SvgAttr.stroke "currentColor"
                        , SvgAttr.strokeWidth "2"
                        , SvgAttr.fill "none"
                        , SvgAttr.strokeLinecap "round"
                        , SvgAttr.strokeLinejoin "round"
                        ]
                        [ Svg.path [ SvgAttr.d "M23 4v6h-6M1 20v-6h6" ] []
                        , Svg.path [ SvgAttr.d "M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" ] []
                        ]
                    , span [] [ text "Refresh" ]
                    ]
                ]
            ]
        ]

renderMain : Model -> Html Msg
renderMain model =
    main_ [ class "dashboard-main" ]
        [ renderAlertBanner model
        , case model.activeTab of
            Overview ->
                renderOverview model

            History ->
                renderHistory model
        ]

renderAlertBanner : Model -> Html Msg
renderAlertBanner model =
    case model.alertMessage of
        Just ( msg, isSuccess ) ->
            div
                [ id "alert-banner"
                , classList [ ( "banner", True ), ( "success", isSuccess ) ]
                ]
                [ span [ class "banner-icon" ] [ text (if isSuccess then "✓" else "ℹ") ]
                , span [ id "alert-msg", class "banner-text" ] [ text msg ]
                , button
                    [ class "close-btn"
                    , style "margin-left" "auto"
                    , style "background" "none"
                    , style "border" "none"
                    , style "color" "inherit"
                    , style "cursor" "pointer"
                    , style "font-size" "1.2rem"
                    , onClick DismissAlert
                    ]
                    [ text "×" ]
                ]

        Nothing ->
            text ""

renderOverview : Model -> Html Msg
renderOverview model =
    section [ id "view-active", class "view-section" ]
        [ if String.isEmpty model.apiKey then
            renderAuthRequiredState
          else if List.isEmpty model.quotas && not model.isFetching then
            renderEmptyState
          else
            div [ id "quota-grid", class "quota-grid" ]
                (if model.isFetching && List.isEmpty model.quotas then
                    [ div [ class "loading-cards" ]
                        [ div [ class "skeleton-card" ] []
                        , div [ class "skeleton-card" ] []
                        , div [ class "skeleton-card" ] []
                        ]
                    ]
                 else
                    List.map (renderQuotaCard model.timeZone model.currentTime model.fetchTime) (List.sortBy Tuple.first model.quotas)
                )
        ]

renderAuthRequiredState : Html Msg
renderAuthRequiredState =
    div [ class "empty-state", style "margin" "0 auto", style "width" "100%" ]
        [ div [ class "empty-icon" ] [ text "🔑" ]
        , h2 [] [ text "Authentication Required" ]
        , p [] [ text "Please click the settings gear or the button below to configure your API key." ]
        , button
            [ class "submit-btn"
            , style "margin-top" "1rem"
            , style "width" "auto"
            , style "padding" "0.6rem 1.5rem"
            , onClick (ShowSettingsModal True)
            ]
            [ text "Configure API Key" ]
        ]

renderEmptyState : Html Msg
renderEmptyState =
    div [ id "empty-state", class "empty-state" ]
        [ div [ class "empty-icon" ] [ text "📭" ]
        , h2 [] [ text "No Quota Data Logged" ]
        , p [] [ text "This service hasn't received any quota updates yet. Log quota updates by sending a POST request or setting up your Antigravity CLI statusline integration." ]
        , div [ class "integration-guide" ]
            [ h3 [] [ text "Quick Integration" ]
            , p [] [ text "Run the statusline script to report your local Antigravity metrics automatically:" ]
            , pre []
                [ code []
                    [ text "curl -X POST \\\n  -H \"X-API-Key: <YOUR_API_KEY>\" \\\n  -H \"Content-Type: application/json\" \\\n  -d '{\"quota\":{\"gemini-flash\":{\"remaining_fraction\":0.85,\"reset_in_seconds\":3600}}}' \\\n  /api/v1/quota" ]
                ]
            ]
        ]

getQuotaState : Float -> String
getQuotaState fraction =
    if fraction <= 0.20 then
        "danger"
    else if fraction <= 0.50 then
        "warning"
    else
        "safe"

renderQuotaCard : Time.Zone -> Time.Posix -> Time.Posix -> ( String, Quota ) -> Html Msg
renderQuotaCard zone now fetchTime ( name, q ) =
    let
        cardState =
            getQuotaState q.remainingFraction

        percentText =
            String.fromFloat (roundFloat (q.remainingFraction * 100) 1) ++ "%"

        -- Gauge calculations
        radius = 50
        circumference = 2 * pi * radius
        strokeDashoffset = circumference * (1 - q.remainingFraction)

        -- Parse reset time
        parsedResetResult = Iso8601.toTime q.resetTime
        
        targetTime =
            case parsedResetResult of
                Ok posix ->
                    -- Check if year is > 2000 to avoid "0001-01-01" zero time
                    if Time.toYear Time.utc posix > 2000 then
                        Just posix
                    else if q.resetInSeconds > 0 then
                        Just (Time.millisToPosix (Time.posixToMillis fetchTime + (q.resetInSeconds * 1000)))
                    else
                        Nothing

                Err _ ->
                    if q.resetInSeconds > 0 then
                        Just (Time.millisToPosix (Time.posixToMillis fetchTime + (q.resetInSeconds * 1000)))
                    else
                        Nothing

        formattedReset =
            case targetTime of
                Just posix ->
                    formatPosix zone posix

                Nothing ->
                    "Never"

        -- Countdown calculation
        countdownText =
            case targetTime of
                Just target ->
                    let
                        diffMs = Time.posixToMillis target - Time.posixToMillis now
                    in
                    if diffMs <= 0 then
                        "00:00:00"
                    else
                        let
                            diffSecs = diffMs // 1000
                            hours = diffSecs // 3600
                            minutes = remainderBy 3600 diffSecs // 60
                            seconds = remainderBy 60 diffSecs
                            pad num = String.padLeft 2 '0' (String.fromInt num)
                        in
                        if hours > 24 then
                            String.fromInt (hours // 24) ++ "d " ++ pad (remainderBy 24 hours) ++ "h " ++ pad minutes ++ "m"
                        else
                            pad hours ++ ":" ++ pad minutes ++ ":" ++ pad seconds

                Nothing ->
                    "Never"

        isExpired =
            case targetTime of
                Just target ->
                    Time.posixToMillis target - Time.posixToMillis now <= 0

                Nothing ->
                    False
    in
    div [ class ("quota-card state-" ++ cardState), attribute "data-quota-name" name ]
        [ div [ class "card-header" ]
            [ span [ class "card-title" ] [ text name ]
            , span [ class "card-badge" ] [ text cardState ]
            ]
        , div [ class "card-gauge-wrapper" ]
            [ Svg.svg [ SvgAttr.class "gauge-svg", SvgAttr.width "130", SvgAttr.height "130" ]
                [ Svg.circle [ SvgAttr.class "gauge-track", SvgAttr.cx "65", SvgAttr.cy "65", SvgAttr.r (String.fromInt radius) ] []
                , Svg.circle
                    [ SvgAttr.class "gauge-value"
                    , SvgAttr.cx "65"
                    , SvgAttr.cy "65"
                    , SvgAttr.r (String.fromInt radius)
                    , SvgAttr.strokeDasharray (String.fromFloat circumference)
                    , SvgAttr.strokeDashoffset (String.fromFloat strokeDashoffset)
                    ]
                    []
                ]
            , div [ class "gauge-percentage" ]
                [ span [] [ text percentText ]
                , span [ class "gauge-label" ] [ text "remaining" ]
                ]
            ]
        , div [ class "card-details" ]
            [ div [ class "detail-row" ]
                [ span [ class "detail-label" ] [ text "Reset Time" ]
                , span [ class "detail-value", title q.resetTime ] [ text formattedReset ]
                ]
            , div [ class "detail-row" ]
                [ span [ class "detail-label" ] [ text "Time Remaining" ]
                , span
                    [ classList [ ( "detail-value", True ), ( "countdown", True ), ( "expired", isExpired ) ]
                    ]
                    [ text countdownText ]
                ]
            ]
        ]

renderHistory : Model -> Html Msg
renderHistory model =
    let
        activeSeries =
            List.filter (\( name, _ ) -> not (Set.member name model.toggledOffQuotas)) model.history
    in
    section [ id "view-history", class "view-section" ]
        [ div [ class "history-view-card" ]
            [ div [ class "history-view-header" ]
                [ div [ class "history-title-group" ]
                    [ h2 [] [ text "Utilization History" ]
                    , div [ id "timeframe-toggle", class "timeframe-toggle" ]
                        [ button
                            [ classList [ ( "time-toggle-btn", True ), ( "active", model.selectedDays == 1 ) ]
                            , onClick (SetSelectedDays 1)
                            ]
                            [ text "24h" ]
                        , button
                            [ classList [ ( "time-toggle-btn", True ), ( "active", model.selectedDays == 7 ) ]
                            , onClick (SetSelectedDays 7)
                            ]
                            [ text "7d" ]
                        ]
                    ]
                , div [ id "chart-legend", class "chart-legend-container" ]
                    (List.map (renderLegendBadge model.toggledOffQuotas model.history) (List.sortBy Tuple.first model.history))
                ]
            , div [ class "chart-content" ]
                [ if model.isFetching && List.isEmpty model.history then
                    div [ id "chart-loading", class "chart-loading-state" ]
                        [ div [ class "loading-spinner" ] []
                        , p [] [ text "Fetching history metrics..." ]
                        ]
                  else if List.isEmpty activeSeries || List.all (\( _, pts ) -> List.isEmpty pts) activeSeries then
                    div [ id "chart-empty", class "chart-empty-state" ]
                        [ p [] [ text ("No history points logged for this quota name in the last " ++ (if model.selectedDays == 1 then "24h" else "7d") ++ ".") ] ]
                  else
                    div [ id "chart-container", class "chart-container", style "position" "relative" ]
                      [ renderHistoryChart model
                      ]
                ]
            ]
        ]

renderLegendBadge : Set String -> List ( String, List HistoryPoint ) -> ( String, List HistoryPoint ) -> Html Msg
renderLegendBadge toggledOff history ( name, _ ) =
    let
        allKeys =
            List.map Tuple.first history |> List.sort

        color =
            getQuotaColor allKeys name

        isDisabled =
            Set.member name toggledOff
    in
    div
        [ classList [ ( "legend-badge", True ), ( "disabled", isDisabled ) ]
        , onClick (ToggleQuotaSeries name)
        ]
        [ span [ class "legend-color-dot", style "background" color ] []
        , span [] [ text name ]
        ]

renderHistoryChart : Model -> Svg Msg
renderHistoryChart model =
    let
        activeSeries =
            List.filter (\( name, _ ) -> not (Set.member name model.toggledOffQuotas)) model.history
                |> List.sortBy Tuple.first

        allHistoryKeys =
            List.map Tuple.first model.history |> List.sort

        chartWidth = 800
        chartHeight = 380
        marginTop = 40
        marginBottom = 50
        marginLeft = 60
        marginRight = 40
        plotWidth = chartWidth - marginLeft - marginRight
        plotHeight = chartHeight - marginTop - marginBottom

        tMax = Time.posixToMillis model.currentTime
        tMin = tMax - (model.selectedDays * 24 * 60 * 60 * 1000)

        -- Coordinate projectors
        projectX t =
            marginLeft + (toFloat (t - tMin) / toFloat (tMax - tMin)) * plotWidth

        projectY val =
            marginTop + (1 - val) * plotHeight

        -- Y-Axis grid lines
        yGridValues = [ 0.0, 0.25, 0.5, 0.75, 1.0 ]
        yGridLines =
            List.map
                (\val ->
                    let
                        y = projectY val
                    in
                    Svg.g []
                        [ Svg.line
                            [ SvgAttr.class "chart-grid-line"
                            , SvgAttr.x1 (String.fromFloat marginLeft)
                            , SvgAttr.y1 (String.fromFloat y)
                            , SvgAttr.x2 (String.fromFloat (chartWidth - marginRight))
                            , SvgAttr.y2 (String.fromFloat y)
                            ]
                            []
                        , Svg.text_
                            [ SvgAttr.class "chart-axis-text y-axis"
                            , SvgAttr.x (String.fromFloat (marginLeft - 10))
                            , SvgAttr.y (String.fromFloat (y + 4))
                            , SvgAttr.textAnchor "end"
                            ]
                            [ text (String.fromInt (round (val * 100)) ++ "%") ]
                        ]
                )
                yGridValues

        -- X-Axis ticks
        numTicks = 5
        xTicks =
            List.map
                (\i ->
                    let
                        ratio = toFloat i / toFloat (numTicks - 1)
                        t = tMin + round (ratio * toFloat (tMax - tMin))
                        x = marginLeft + ratio * plotWidth
                        tickTime = Time.millisToPosix t
                    in
                    Svg.g []
                        [ if i > 0 && i < numTicks - 1 then
                            Svg.line
                                [ SvgAttr.class "chart-grid-line"
                                , SvgAttr.x1 (String.fromFloat x)
                                , SvgAttr.y1 (String.fromFloat marginTop)
                                , SvgAttr.x2 (String.fromFloat x)
                                , SvgAttr.y2 (String.fromFloat (chartHeight - marginBottom))
                                ]
                                []
                          else
                            Svg.g [] []
                        , Svg.text_
                            [ SvgAttr.class "chart-axis-text x-axis"
                            , SvgAttr.x (String.fromFloat x)
                            , SvgAttr.y (String.fromFloat (chartHeight - marginBottom + 20))
                            , SvgAttr.textAnchor "middle"
                            ]
                            [ text (formatTickTime model.timeZone model.selectedDays tickTime) ]
                        ]
                )
                (List.range 0 (numTicks - 1))

        -- Project data points for each series
        projectPoints pts =
            List.filterMap
                (\pt ->
                    case Iso8601.toTime pt.timestamp of
                        Ok posix ->
                            let
                                t = Time.posixToMillis posix
                            in
                            if t <= tMax then
                                Just
                                    { x = projectX t
                                    , y = projectY pt.value
                                    , t = t
                                    , val = pt.value
                                    , isReset = pt.isReset
                                    }
                            else
                                Nothing

                        Err _ ->
                            Nothing
                )
                pts
                |> List.sortBy .t

        seriesProjected =
            List.map (\( name, pts ) -> ( name, projectPoints pts )) activeSeries

        -- Paths
        renderSeriesPath ( name, pts ) =
            case pts of
                [] ->
                    Svg.g [] []

                first :: rest ->
                    let
                        color =
                            getQuotaColor allHistoryKeys name

                        -- Step-after formatting
                        folder pt ( acc, prev ) =
                            ( acc ++ " L " ++ String.fromFloat pt.x ++ " " ++ String.fromFloat prev.y ++ " L " ++ String.fromFloat pt.x ++ " " ++ String.fromFloat pt.y
                            , pt
                            )

                        ( pathD, _ ) =
                            List.foldl folder ( "M " ++ String.fromFloat first.x ++ " " ++ String.fromFloat first.y, first ) rest

                        linePath =
                            Svg.path
                                [ SvgAttr.d pathD
                                , SvgAttr.class "chart-line-path"
                                , SvgAttr.stroke color
                                , SvgAttr.fill "none"
                                , SvgAttr.filter "url(#chart-glow)"
                                ]
                                []

                        dots =
                            List.map
                                (\pt ->
                                    if pt.isReset then
                                        Svg.circle
                                            [ SvgAttr.class "chart-data-point chart-reset-point"
                                            , SvgAttr.cx (String.fromFloat pt.x)
                                            , SvgAttr.cy (String.fromFloat pt.y)
                                            , SvgAttr.r "5.0"
                                            , SvgAttr.fill color
                                            , SvgAttr.stroke "#ffffff"
                                            , SvgAttr.strokeWidth "2"
                                            , style "fill" color
                                            , style "stroke" "#ffffff"
                                            , style "stroke-width" "2"
                                            ]
                                            []
                                    else
                                        Svg.circle
                                            [ SvgAttr.class "chart-data-point"
                                            , SvgAttr.cx (String.fromFloat pt.x)
                                            , SvgAttr.cy (String.fromFloat pt.y)
                                            , SvgAttr.r "2.2"
                                            , SvgAttr.stroke color
                                            , SvgAttr.fill "#141124"
                                            ]
                                            []
                                )
                                pts
                    in
                    Svg.g []
                        (linePath :: dots)

        seriesPaths =
            List.map renderSeriesPath seriesProjected



        -- Hover/Tracker logic
        trackerElements =
            case model.hoverX of
                Just hx ->
                    if hx < toFloat marginLeft || hx > toFloat (chartWidth - marginRight) then
                        []
                    else
                        let
                            -- Map hover X back to millisecond timestamp
                            hoverT = tMin + round (((hx - toFloat marginLeft) / toFloat plotWidth) * toFloat (tMax - tMin))

                            -- Find closest point in each active series within a distance threshold
                            closestPointsInSeries =
                                List.filterMap
                                    (\( name, pts ) ->
                                        case pts of
                                            [] ->
                                                Nothing

                                            _ ->
                                                let
                                                    sortedByDist =
                                                        List.sortBy (\pt -> abs (pt.t - hoverT)) pts
                                                in
                                                case List.head sortedByDist of
                                                    Just bestPt ->
                                                        -- 60px pixel distance threshold
                                                        let
                                                            distPx = abs (bestPt.x - hx)
                                                        in
                                                        if distPx < 60 then
                                                            Just ( name, bestPt, distPx )
                                                        else
                                                            Nothing

                                                    Nothing ->
                                                        Nothing
                                    )
                                    seriesProjected

                            sortedByTotalDist =
                                List.sortBy (\( _, _, dist ) -> dist) closestPointsInSeries
                        in
                        case List.head sortedByTotalDist of
                            Just ( _, referencePt, _ ) ->
                                let
                                    alignedX = referencePt.x
                                    alignedT = referencePt.t

                                    -- vertical tracker line
                                    vLine =
                                        Svg.line
                                            [ SvgAttr.x1 (String.fromFloat alignedX)
                                            , SvgAttr.y1 (String.fromFloat marginTop)
                                            , SvgAttr.x2 (String.fromFloat alignedX)
                                            , SvgAttr.y2 (String.fromFloat (chartHeight - marginBottom))
                                            , SvgAttr.stroke "rgba(255, 255, 255, 0.18)"
                                            , SvgAttr.strokeWidth "1.5"
                                            , SvgAttr.strokeDasharray "3 3"
                                            ]
                                            []

                                    -- crosshair dots
                                    dots =
                                        List.map
                                            (\( name, pt, _ ) ->
                                                let
                                                    color = getQuotaColor allHistoryKeys name
                                                in
                                                Svg.circle
                                                    [ SvgAttr.cx (String.fromFloat alignedX)
                                                    , SvgAttr.cy (String.fromFloat pt.y)
                                                    , SvgAttr.r "5.5"
                                                    , SvgAttr.fill color
                                                    , SvgAttr.stroke "#141124"
                                                    , SvgAttr.strokeWidth "2"
                                                    ]
                                                    []
                                            )
                                            closestPointsInSeries
                                in
                                vLine :: dots

                            Nothing ->
                                []

                Nothing ->
                    []

        -- Render Tooltip SVG overlaid on chart container
        tooltipSvg =
            case model.hoverX of
                Just hx ->
                    if hx < toFloat marginLeft || hx > toFloat (chartWidth - marginRight) then
                        text ""
                    else
                        let
                            hoverT = tMin + round (((hx - toFloat marginLeft) / toFloat plotWidth) * toFloat (tMax - tMin))

                            closestPointsInSeries =
                                List.filterMap
                                    (\( name, pts ) ->
                                        case pts of
                                            [] ->
                                                Nothing

                                            _ ->
                                                let
                                                    sortedByDist =
                                                        List.sortBy (\pt -> abs (pt.t - hoverT)) pts
                                                in
                                                case List.head sortedByDist of
                                                    Just bestPt ->
                                                        let
                                                            distPx = abs (bestPt.x - hx)
                                                        in
                                                        if distPx < 60 then
                                                            Just ( name, bestPt, distPx )
                                                        else
                                                            Nothing

                                                    Nothing ->
                                                        Nothing
                                    )
                                    seriesProjected

                            sortedByTotalDist =
                                List.sortBy (\( _, _, dist ) -> dist) closestPointsInSeries
                        in
                        case List.head sortedByTotalDist of
                            Just ( _, referencePt, _ ) ->
                                let
                                    alignedX = referencePt.x
                                    alignedT = referencePt.t

                                    tooltipValues =
                                        List.sortBy (\( name, _, _ ) -> name) closestPointsInSeries
                                            |> List.map
                                                (\( name, pt, _ ) ->
                                                    let
                                                        color = getQuotaColor allHistoryKeys name
                                                        labelSuffix = if pt.isReset then " (Reset)" else ""
                                                    in
                                                    div [ class "chart-tooltip-value" ]
                                                        [ span [ class "chart-tooltip-marker", style "background" color ] []
                                                        , span [ style "color" "var(--text-secondary)", style "margin-right" "0.5rem" ] [ text (name ++ ":") ]
                                                        , strong [] [ text (String.fromFloat (roundFloat (pt.val * 100) 1) ++ "%" ++ labelSuffix) ]
                                                        ]
                                                )
                                    -- Position calculation matching JS logic
                                    -- Chart container has width 800
                                    tooltipWidth = 180
                                    tooltipHeight = 22 + (24 * List.length closestPointsInSeries)
                                    
                                    leftPosition =
                                        let
                                            rawLeft = alignedX + 15
                                        in
                                        if rawLeft + toFloat tooltipWidth > 800 - 10 then
                                            let
                                                flipped = alignedX - toFloat tooltipWidth - 15
                                            in
                                            if flipped < 10 then 10 else flipped
                                        else
                                            rawLeft

                                    avgY =
                                        let
                                            sumY = List.foldl (\( _, pt, _ ) acc -> acc + pt.y) 0.0 closestPointsInSeries
                                            len = List.length closestPointsInSeries
                                        in
                                        if len == 0 then 0.0 else sumY / toFloat len

                                    topPosition =
                                        let
                                            rawTop = avgY - toFloat tooltipHeight - 10
                                        in
                                        if rawTop < 10 then
                                            avgY + 15
                                        else
                                            rawTop
                                in
                                Svg.foreignObject
                                    [ SvgAttr.x (String.fromFloat leftPosition)
                                    , SvgAttr.y (String.fromFloat topPosition)
                                    , SvgAttr.width "240"
                                    , SvgAttr.height "200"
                                    , style "pointer-events" "none"
                                    ]
                                    [ div
                                        [ id "chart-tooltip"
                                        , class "chart-tooltip"
                                        , style "opacity" "1"
                                        , style "transform" "none"
                                        , style "position" "static"
                                        , style "min-width" "180px"
                                        , style "width" "max-content"
                                        , style "white-space" "nowrap"
                                        , style "border-color" "rgba(255, 255, 255, 0.15)"
                                        ]
                                        [ div [ class "chart-tooltip-time" ] [ text (formatTooltipTime model.timeZone (Time.millisToPosix alignedT)) ]
                                        , div [] tooltipValues
                                        ]
                                    ]

                            Nothing ->
                                Svg.g [] []

                Nothing ->
                    Svg.g [] []
    in
    div [ style "position" "relative" ]
        [ Svg.svg
            [ SvgAttr.viewBox ("0 0 " ++ String.fromInt chartWidth ++ " " ++ String.fromInt chartHeight)
            , SvgAttr.class "chart-svg"
            ]
            [ -- Glow filter
              Svg.defs []
                [ Svg.filter
                    [ SvgAttr.id "chart-glow"
                    , SvgAttr.x "-10%"
                    , SvgAttr.y "-10%"
                    , SvgAttr.width "120%"
                    , SvgAttr.height "120%"
                    ]
                    [ Svg.feGaussianBlur [ SvgAttr.stdDeviation "3", SvgAttr.result "blur" ] []
                    , Svg.feMerge []
                        [ Svg.feMergeNode [ SvgAttr.in_ "blur" ] []
                        , Svg.feMergeNode [ SvgAttr.in_ "SourceGraphic" ] []
                        ]
                    ]
                ]
            -- Grid elements
            , Svg.g [] yGridLines
            , Svg.g [] xTicks
            -- Lines and dots
            , Svg.g [] seriesPaths
            -- Mouse tracker lines and dots
            , Svg.g [] trackerElements
            , tooltipSvg
            ]
        ]

renderModal : Model -> Html Msg
renderModal model =
    div
        [ id "settings-modal"
        , classList [ ( "modal", True ), ( "hidden", not model.showSettingsModal ) ]
        ]
        [ div [ id "modal-overlay", class "modal-overlay", onClick (ShowSettingsModal False) ] []
        , div [ class "modal-content" ]
            [ div [ class "modal-header" ]
                [ h2 [] [ text "API Configuration" ]
                , button
                    [ id "close-modal-btn"
                    , class "close-btn"
                    , attribute "aria-label" "Close modal"
                    , onClick (ShowSettingsModal False)
                    ]
                    [ text "×" ]
                ]
            , div [ class "modal-body" ]
                [ Html.form [ id "settings-form", onSubmit SubmitSettings ]
                    [ div [ class "form-group" ]
                        [ label [ for "api-key-input" ] [ text "X-API-Key" ]
                        , input
                            [ id "api-key-input"
                            , type_ "password"
                            , placeholder "Enter your secret API key..."
                            , required True
                            , autocomplete False
                            , value model.tempApiKey
                            , onInput InputApiKey
                            ]
                            []
                        , small [ class "form-help" ]
                            [ text "Enter the key configured via "
                            , code [] [ text "QUOTA_API_KEY" ]
                            , text " on your server (defaults to "
                            , code [] [ text "default-secret-key" ]
                            , text " locally)."
                            ]
                        ]
                    , div [ class "form-group checkbox-group" ]
                        [ input
                            [ id "save-key-checkbox"
                            , type_ "checkbox"
                            , checked model.tempSaveKey
                            , onCheck CheckboxSaveKey
                            ]
                            []
                        , label [ for "save-key-checkbox" ] [ text "Remember API key in this browser" ]
                        ]
                    , button [ type_ "submit", id "save-settings-btn", class "submit-btn" ] [ text "Save & Connect" ]
                    ]
                ]
            ]
        ]

renderFooter : Model -> Html Msg
renderFooter model =
    footer [ class "dashboard-footer" ]
        [ div [ class "footer-container" ]
            [ p [] [ text "© 2026 Antigravity Quota Monitor. All rights reserved." ]
            , div [ class "footer-links" ]
                [ span [ class "auto-refresh-control" ]
                    [ label [ for "auto-refresh-select" ] [ text "Auto-refresh:" ]
                    , select
                        [ id "auto-refresh-select"
                        , value (String.fromInt model.autoRefreshInterval)
                        , onInput (\val -> ChangeAutoRefresh (Maybe.withDefault 0 (String.toInt val)))
                        ]
                        [ option [ value "10" ] [ text "Every 10s" ]
                        , option [ value "30" ] [ text "Every 30s" ]
                        , option [ value "60" ] [ text "Every 1m" ]
                        , option [ value "0" ] [ text "Disabled" ]
                        ]
                    ]
                ]
            ]
        ]

-- DATE FORMATTING HELPERS
formatPosix : Time.Zone -> Time.Posix -> String
formatPosix zone posix =
    let
        monthStr =
            case Time.toMonth zone posix of
                Time.Jan -> "Jan"
                Time.Feb -> "Feb"
                Time.Mar -> "Mar"
                Time.Apr -> "Apr"
                Time.May -> "May"
                Time.Jun -> "Jun"
                Time.Jul -> "Jul"
                Time.Aug -> "Aug"
                Time.Sep -> "Sep"
                Time.Oct -> "Oct"
                Time.Nov -> "Nov"
                Time.Dec -> "Dec"

        day = String.fromInt (Time.toDay zone posix)
        hour24 = Time.toHour zone posix
        ampm = if hour24 >= 12 then "PM" else "AM"
        hour12Raw = remainderBy 12 hour24
        hour12 = if hour12Raw == 0 then 12 else hour12Raw
        hourStr = String.padLeft 2 '0' (String.fromInt hour12)
        minuteStr = String.padLeft 2 '0' (String.fromInt (Time.toMinute zone posix))
    in
    monthStr ++ " " ++ day ++ ", " ++ hourStr ++ ":" ++ minuteStr ++ " " ++ ampm

formatTickTime : Time.Zone -> Int -> Time.Posix -> String
formatTickTime zone selectedDays posix =
    if selectedDays == 7 then
        let
            monthStr =
                case Time.toMonth zone posix of
                    Time.Jan -> "Jan"
                    Time.Feb -> "Feb"
                    Time.Mar -> "Mar"
                    Time.Apr -> "Apr"
                    Time.May -> "May"
                    Time.Jun -> "Jun"
                    Time.Jul -> "Jul"
                    Time.Aug -> "Aug"
                    Time.Sep -> "Sep"
                    Time.Oct -> "Oct"
                    Time.Nov -> "Nov"
                    Time.Dec -> "Dec"
            day = String.fromInt (Time.toDay zone posix)
        in
        monthStr ++ " " ++ day
    else
        let
            h = String.padLeft 2 '0' (String.fromInt (Time.toHour zone posix))
            m = String.padLeft 2 '0' (String.fromInt (Time.toMinute zone posix))
        in
        h ++ ":" ++ m

formatTooltipTime : Time.Zone -> Time.Posix -> String
formatTooltipTime zone posix =
    let
        monthStr =
            case Time.toMonth zone posix of
                Time.Jan -> "Jan"
                Time.Feb -> "Feb"
                Time.Mar -> "Mar"
                Time.Apr -> "Apr"
                Time.May -> "May"
                Time.Jun -> "Jun"
                Time.Jul -> "Jul"
                Time.Aug -> "Aug"
                Time.Sep -> "Sep"
                Time.Oct -> "Oct"
                Time.Nov -> "Nov"
                Time.Dec -> "Dec"

        day = String.fromInt (Time.toDay zone posix)
        h = String.padLeft 2 '0' (String.fromInt (Time.toHour zone posix))
        m = String.padLeft 2 '0' (String.fromInt (Time.toMinute zone posix))
        s = String.padLeft 2 '0' (String.fromInt (Time.toSecond zone posix))
    in
    monthStr ++ " " ++ day ++ " " ++ h ++ ":" ++ m ++ ":" ++ s

-- NUMERIC HELPERS
roundFloat : Float -> Int -> Float
roundFloat value decimals =
    let
        factor = toFloat (10 ^ decimals)
    in
    toFloat (round (value * factor)) / factor

colors : List String
colors =
    [ "#00f2fe", "#ff007f", "#39ff14", "#f1c40f", "#9b59b6", "#e67e22" ]

getQuotaColor : List String -> String -> String
getQuotaColor allKeys name =
    let
        indexed =
            List.indexedMap Tuple.pair allKeys

        matchingIdx =
            List.filter (\( _, k ) -> k == name) indexed
                |> List.head
                |> Maybe.map Tuple.first
                |> Maybe.withDefault 0
    in
    List.drop (remainderBy (List.length colors) matchingIdx) colors
        |> List.head
        |> Maybe.withDefault "#00f2fe"

-- MAIN
main : Program Flags Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , subscriptions = subscriptions
        , view = view
        }

subscriptions : Model -> Sub Msg
subscriptions model =
    let
        countdownSub =
            Time.every 1000 Tick

        autoRefreshSub =
            if model.autoRefreshInterval > 0 && not (String.isEmpty model.apiKey) then
                Time.every (toFloat model.autoRefreshInterval * 1000) (\_ -> TriggerRefresh)
            else
                Sub.none
    in
    Sub.batch
        [ countdownSub
        , autoRefreshSub
        , onChartMouseMove MouseMove
        , onChartMouseLeave (\_ -> MouseLeave)
        ]
