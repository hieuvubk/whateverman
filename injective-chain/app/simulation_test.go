package app

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/log"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	evidencetypes "github.com/cosmos/cosmos-sdk/x/evidence/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v7/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v7/modules/core/exported"
)

func init() {
	simcli.GetSimulatorFlags()
}

type storeKeysPrefixes struct {
	A        storetypes.StoreKey
	B        storetypes.StoreKey
	Prefixes [][]byte
}

// fauxMerkleModeOpt returns a BaseApp option to use a dbStoreAdapter instead of
// an IAVLStore for faster simulation speed.
func fauxMerkleModeOpt(bapp *baseapp.BaseApp) {
	bapp.SetFauxMerkleMode()
}

func TestFullAppSimulation(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = "simulation-app"

	db, dir, logger, skip, err := sims.SetupSimulation(
		config,
		"leveldb-app-sim",
		"Simulation",
		simcli.FlagVerboseValue,
		simcli.FlagEnabledValue,
	)
	if skip {
		t.Skip("skipping application simulation")
	}
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		db.Close()
		require.NoError(t, os.RemoveAll(dir))
	}()

	app := NewInjectiveApp(logger, db, nil, true, map[int64]bool{}, DefaultNodeHome, simcli.FlagPeriodValue, MakeEncodingConfig(), sims.EmptyAppOptions{})
	require.Equal(t, appName, app.Name())

	// run randomized simulation
	_, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		app.BaseApp,
		sims.AppStateFn(app.AppCodec(), app.SimulationManager(), NewDefaultGenesisState()),
		simtypes.RandomAccounts, // Replace with own random account function if using keys other than secp256k1
		sims.SimulationOperations(app, app.AppCodec(), config),
		app.ModuleAccountAddrs(),
		config,
		app.appCodec,
	)

	// export state and simParams before the simulation error is checked
	err = sims.CheckExportSimulation(app, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)

	if config.Commit {
		sims.PrintStats(db)
	}
}

func TestAppImportExport(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = "simulation-app"

	db, dir, logger, skip, err := sims.SetupSimulation(
		config,
		"leveldb-app-sim",
		"Simulation",
		simcli.FlagVerboseValue,
		simcli.FlagEnabledValue,
	)
	if skip {
		t.Skip("skipping application import/export simulation")
	}
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		db.Close()
		require.NoError(t, os.RemoveAll(dir))
	}()

	app := NewInjectiveApp(logger, db, nil, true, map[int64]bool{}, DefaultNodeHome, simcli.FlagPeriodValue, MakeEncodingConfig(), sims.EmptyAppOptions{})
	require.Equal(t, appName, app.Name())

	// Run randomized simulation
	_, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		app.BaseApp,
		sims.AppStateFn(app.AppCodec(), app.SimulationManager(), NewDefaultGenesisState()),
		simtypes.RandomAccounts, // Replace with own random account function if using keys other than secp256k1
		sims.SimulationOperations(app, app.AppCodec(), config),
		app.ModuleAccountAddrs(),
		config,
		app.appCodec,
	)

	// export state and simParams before the simulation error is checked
	err = sims.CheckExportSimulation(app, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)

	if config.Commit {
		sims.PrintStats(db)
	}

	fmt.Printf("exporting genesis...\n")

	exported, err := app.ExportAppStateAndValidators(false, []string{}, []string{})
	require.NoError(t, err)

	fmt.Printf("importing genesis...\n")

	config2 := simcli.NewConfigFromFlags()
	config2.ChainID = "simulation-app-2"

	// nolint: dogsled
	newDB, newDir, _, _, err := sims.SetupSimulation(
		config2,
		"leveldb-app-sim-2",
		"Simulation-2",
		simcli.FlagVerboseValue,
		simcli.FlagEnabledValue,
	)
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		newDB.Close()
		require.NoError(t, os.RemoveAll(newDir))
	}()

	newApp := NewInjectiveApp(log.NewNopLogger(), newDB, nil, true, map[int64]bool{}, DefaultNodeHome, simcli.FlagPeriodValue, MakeEncodingConfig(), sims.EmptyAppOptions{})
	require.Equal(t, appName, newApp.Name())

	var genesisState GenesisState
	err = json.Unmarshal(exported.AppState, &genesisState)
	require.NoError(t, err)

	ctxA := app.NewContext(true, tmproto.Header{Height: app.LastBlockHeight()})
	ctxB := newApp.NewContext(true, tmproto.Header{Height: app.LastBlockHeight()})
	newApp.mm.InitGenesis(ctxB, app.AppCodec(), genesisState)
	newApp.StoreConsensusParams(ctxB, exported.ConsensusParams)

	fmt.Printf("comparing stores...\n")

	storeKeysPrefixes := []storeKeysPrefixes{
		{app.keys[authtypes.StoreKey], newApp.keys[authtypes.StoreKey], [][]byte{}},
		{app.keys[stakingtypes.StoreKey], newApp.keys[stakingtypes.StoreKey],
			[][]byte{
				stakingtypes.UnbondingQueueKey, stakingtypes.RedelegationQueueKey, stakingtypes.ValidatorQueueKey,
				stakingtypes.HistoricalInfoKey,
			}}, // ordering may change but it doesn't matter
		{app.keys[slashingtypes.StoreKey], newApp.keys[slashingtypes.StoreKey], [][]byte{}},
		{app.keys[minttypes.StoreKey], newApp.keys[minttypes.StoreKey], [][]byte{}},
		{app.keys[distrtypes.StoreKey], newApp.keys[distrtypes.StoreKey], [][]byte{}},
		{app.keys[banktypes.StoreKey], newApp.keys[banktypes.StoreKey], [][]byte{banktypes.BalancesPrefix}},
		{app.keys[paramtypes.StoreKey], newApp.keys[paramtypes.StoreKey], [][]byte{}},
		{app.keys[govtypes.StoreKey], newApp.keys[govtypes.StoreKey], [][]byte{}},
		{app.keys[evidencetypes.StoreKey], newApp.keys[evidencetypes.StoreKey], [][]byte{}},
		{app.keys[capabilitytypes.StoreKey], newApp.keys[capabilitytypes.StoreKey], [][]byte{}},
		{app.keys[ibcexported.StoreKey], newApp.keys[ibcexported.StoreKey], [][]byte{}},
		{app.keys[ibctransfertypes.StoreKey], newApp.keys[ibctransfertypes.StoreKey], [][]byte{}},
	}

	for _, skp := range storeKeysPrefixes {
		storeA := ctxA.KVStore(skp.A)
		storeB := ctxB.KVStore(skp.B)

		failedKVAs, failedKVBs := sdk.DiffKVStores(storeA, storeB, skp.Prefixes)
		require.Equal(t, len(failedKVAs), len(failedKVBs), "unequal sets of key-values to compare")

		fmt.Printf("compared %d different key/value pairs between %s and %s\n", len(failedKVAs), skp.A, skp.B)
		require.Equal(t, len(failedKVAs), 0, sims.GetSimulationLog(skp.A.Name(), app.SimulationManager().StoreDecoders, failedKVAs, failedKVBs))
	}
}

func TestAppSimulationAfterImport(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = "simulation-app"

	db, dir, logger, skip, err := sims.SetupSimulation(
		config,
		"leveldb-app-sim",
		"Simulation",
		simcli.FlagVerboseValue,
		simcli.FlagEnabledValue,
	)
	if skip {
		t.Skip("skipping application simulation after import")
	}
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		db.Close()
		require.NoError(t, os.RemoveAll(dir))
	}()

	app := NewInjectiveApp(logger, db, nil, true, map[int64]bool{}, DefaultNodeHome, simcli.FlagPeriodValue, MakeEncodingConfig(), sims.EmptyAppOptions{})
	require.Equal(t, appName, app.Name())

	// Run randomized simulation
	stopEarly, simParams, simErr := simulation.SimulateFromSeed(
		t,
		os.Stdout,
		app.BaseApp,
		sims.AppStateFn(app.AppCodec(), app.SimulationManager(), NewDefaultGenesisState()),
		simtypes.RandomAccounts, // Replace with own random account function if using keys other than secp256k1
		sims.SimulationOperations(app, app.AppCodec(), config),
		app.ModuleAccountAddrs(),
		config,
		app.appCodec,
	)

	// export state and simParams before the simulation error is checked
	err = sims.CheckExportSimulation(app, config, simParams)
	require.NoError(t, err)
	require.NoError(t, simErr)

	if config.Commit {
		sims.PrintStats(db)
	}

	if stopEarly {
		fmt.Println("can't export or import a zero-validator genesis, exiting test...")
		return
	}

	fmt.Printf("exporting genesis...\n")

	exported, err := app.ExportAppStateAndValidators(true, []string{}, []string{})
	require.NoError(t, err)

	fmt.Printf("importing genesis...\n")

	config2 := simcli.NewConfigFromFlags()
	config2.ChainID = "simulation-app"

	newDB, newDir, _, _, err := sims.SetupSimulation(
		config2,
		"leveldb-app-sim-2",
		"Simulation-2",
		simcli.FlagVerboseValue,
		simcli.FlagEnabledValue,
	)
	require.NoError(t, err, "simulation setup failed")

	defer func() {
		newDB.Close()
		require.NoError(t, os.RemoveAll(newDir))
	}()

	newApp := NewInjectiveApp(log.NewNopLogger(), newDB, nil, true, map[int64]bool{}, DefaultNodeHome, simcli.FlagPeriodValue, MakeEncodingConfig(), sims.EmptyAppOptions{})
	require.Equal(t, appName, newApp.Name())

	newApp.InitChain(abci.RequestInitChain{
		AppStateBytes: exported.AppState,
	})

	_, _, err = simulation.SimulateFromSeed(
		t,
		os.Stdout,
		newApp.BaseApp,
		sims.AppStateFn(app.AppCodec(), app.SimulationManager(), NewDefaultGenesisState()),
		simtypes.RandomAccounts, // Replace with own random account function if using keys other than secp256k1
		sims.SimulationOperations(newApp, newApp.AppCodec(), config),
		app.ModuleAccountAddrs(),
		config,
		app.appCodec,
	)
	require.NoError(t, err)
}

// TODO: Make another test for the fuzzer itself, which just has noOp txs
// and doesn't depend on the application.
func TestAppStateDeterminism(t *testing.T) {
	if !simcli.FlagEnabledValue {
		t.Skip("skipping application simulation")
	}

	config := simcli.NewConfigFromFlags()
	config.InitialBlockHeight = 1
	config.ExportParamsPath = ""
	config.OnOperation = false
	config.AllInvariants = false
	config.ChainID = "simulation-app"

	numSeeds := 3
	numTimesToRunPerSeed := 5
	appHashList := make([]json.RawMessage, numTimesToRunPerSeed)

	for i := 0; i < numSeeds; i++ {
		config.Seed = rand.Int63()

		for j := 0; j < numTimesToRunPerSeed; j++ {
			var logger log.Logger
			if simcli.FlagVerboseValue {
				logger = log.TestingLogger()
			} else {
				logger = log.NewNopLogger()
			}

			db := dbm.NewMemDB()
			app := NewInjectiveApp(logger, db, nil, true, map[int64]bool{}, DefaultNodeHome, simcli.FlagPeriodValue, MakeEncodingConfig(), sims.EmptyAppOptions{})

			fmt.Printf(
				"running non-determinism simulation; seed %d: %d/%d, attempt: %d/%d\n",
				config.Seed, i+1, numSeeds, j+1, numTimesToRunPerSeed,
			)

			_, _, err := simulation.SimulateFromSeed(
				t,
				os.Stdout,
				app.BaseApp,
				sims.AppStateFn(app.AppCodec(), app.SimulationManager(), NewDefaultGenesisState()),
				simtypes.RandomAccounts, // Replace with own random account function if using keys other than secp256k1
				sims.SimulationOperations(app, app.AppCodec(), config),
				app.ModuleAccountAddrs(),
				config,
				app.appCodec,
			)
			require.NoError(t, err)

			if config.Commit {
				sims.PrintStats(db)
			}

			appHash := app.LastCommitID().Hash
			appHashList[j] = appHash

			if j != 0 {
				require.Equal(
					t, string(appHashList[0]), string(appHashList[j]),
					"non-determinism in seed %d: %d/%d, attempt: %d/%d\n", config.Seed, i+1, numSeeds, j+1, numTimesToRunPerSeed,
				)
			}
		}
	}
}
